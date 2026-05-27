# Lofi Stream

Authenticated lofi audio streaming system built for a work trial. A single
Go/Chi auth service issues JWTs and generates per-segment Cloudflare R2
presigned URLs on login. The React frontend uses HLS.js to stream audio
directly from R2 — no audio data passes through the auth service. Designed
to the constraint of 100k concurrent users; Cloudflare's CDN handles
segment delivery at any scale.

Architectural decisions including alternatives considered and pros/cons for each choice are documented in ARCHITECTURAL_DECISIONS.md.

## Architecture

```
Client  →  POST /auth/token
        ←  JWT + stream_url (base64 data URL containing rewritten M3U8)

HLS.js  →  loads stream_url (data URL, no network request)
        →  fetches seg000.aac … seg013.aac directly from Cloudflare R2
               each URL carries SigV4 signature as query params
        ←  R2 validates signature at edge, serves segment
```

```
     Browser               Auth Service          Cloudflare R2
    (port 5173)            (port 8080)           (global edge)
         │                      │                      │
         │  POST /auth/token     │                      │
         │  {username,password}  │                      │
         │──────────────────────>│                      │
         │                       │  GetObject           │
         │                       │  playlist.m3u8       │
         │                       │─────────────────────>│
         │                       │<─────────────────────│
         │                       │  (raw M3U8 bytes)    │
         │                       │                      │
         │                       │  PresignGetObject ×14│
         │                       │  (local SDK, no I/O) │
         │                       │                      │
         │  {token, stream_url}  │                      │
         │<──────────────────────│                      │
         │  stream_url =         │                      │
         │  data:...;base64,...  │                      │
         │  (M3U8 with 14        │                      │
         │   presigned URLs)     │                      │
         │                       │                      │
         │  HLS.js loads data URL (no network request)  │
         │                       │                      │
         │  GET seg000.aac?X-Amz-Signature=…            │
         │──────────────────────────────────────────────>
         │<──────────────────────────────────────────────
         │  200 audio/aac (SigV4 validated at edge)     │
         │  ⟨ seg001 … seg013 ⟩  │                      │
         │                       │                      │
         │  [ended event fires]  │                      │
         │  hls.loadSource() ─── loop ─────────────────>│
```

**AD #1 — HLS over chunked HTTP or WebRTC.**
HLS splits audio into static segments served over plain HTTP. Static files
are CDN-cacheable. At 100k users the CDN serves cached segments; the origin
handles only auth, which is lightweight and infrequent. Chunked HTTP cannot
be CDN-cached and holds a persistent connection per user. WebRTC requires SFU
infrastructure and is designed for bidirectional real-time communication —
overkill for pre-recorded audio with no latency requirement.

**AD #2 — Cloudflare R2 replaces any local stream service.**
There is no `backend/stream/` directory. Cloudflare R2 serves all audio
segments in demo and production alike. Zero egress cost is the decisive
factor ($0 vs ~$2,000/full stream cycle on AWS CloudFront at 100k users).
R2 uses the S3-compatible API so AWS SDK v2 works without modification.
The demo connects to the live Cloudflare account; no local file server exists.

```
                    WHAT RUNS WHERE AT 100k USERS

  ┌──────────────────────────────────────────────────────────┐
  │  LOCAL                                                   │
  │                                                          │
  │  Auth service   one JSON response per login              │
  │  (port 8080)    100k users / 4h window ≈ 7 req/s        │
  │                 handles zero audio bytes                 │
  │                                                          │
  │  Frontend       static files, loaded once                │
  │  (port 5173)                                             │
  └─────────────────────────┬────────────────────────────────┘
                             │ tiny JSON per session
                             │ 0 bytes of audio traffic
                             ▼
  ┌──────────────────────────────────────────────────────────┐
  │  CLOUDFLARE  (300+ edge nodes, global)                   │
  │                                                          │
  │  Stores  15 files  (1× M3U8 + 14× AAC segments)         │
  │  Serves  100,000 concurrent streams                      │
  │          100k × 128 kbps = 12.8 Gbps total              │
  │  Caches  segments per node after first request           │
  │  Checks  SigV4 on every segment before serving bytes     │
  │  Cost    $0 egress                                       │
  └──────────────────────────────────────────────────────────┘
```

**AD #4 + AD #8 — Two tokens, one auth event.**
HLS.js cannot add `Authorization` headers to segment requests without
breaking CDN caching. The auth service solves this by fetching the static
`playlist.m3u8` from R2, rewriting every segment filename with an
individually SigV4-presigned URL, and returning the result as a base64
data URL in `stream_url`. HLS.js loads the data URL directly; each segment
request carries its own signature as query parameters. R2 validates at the
edge. The JWT in `token` is separate and gates only the admin API via
`Authorization: Bearer` header.

```
  ┌─────────────────────────────────┐  ┌─────────────────────────────────┐
  │             JWT                 │  │     Presigned URL  (×14)        │
  ├─────────────────────────────────┤  ├─────────────────────────────────┤
  │ Issued by:  auth service        │  │ Issued by:  auth service        │
  │ Validated:  jwt.go middleware   │  │ Validated:  Cloudflare R2 edge  │
  │                                 │  │                                 │
  │ Sent via:   Authorization:      │  │ Sent via:   URL query params    │
  │             Bearer <token>      │  │             X-Amz-Signature=…   │
  │                                 │  │                                 │
  │ Protects:   /admin/tracks       │  │ Protects:   seg000…seg013.aac   │
  │             GET POST DELETE     │  │             one URL per segment │
  │                                 │  │                                 │
  │ Expiry:     TOKEN_EXPIRY_HOURS  │  │ Expiry:     TOKEN_EXPIRY_HOURS  │
  │             (default 4h)        │  │             (same as JWT)       │
  └─────────────────────────────────┘  └─────────────────────────────────┘
        Both issued in a single POST /auth/token. Expire at the same time.
                    Re-login required after TOKEN_EXPIRY_HOURS.
```

**AD #5 — Client-side infinite loop.**
The static M3U8 has `EXT-X-ENDLIST`. When the audio element fires `ended`,
the React player calls `hls.loadSource(streamUrl)` and `play()` — the loop
is one event listener with two lines. No server-side state, no playlist
endpoint, no stitching logic. Gapless by construction: FFmpeg places segment
boundaries at clean AAC frame boundaries.

**AD #7 — JSON file + InMemoryLibrary + ContentLibrary interface.**
`tracks.json` is loaded at startup into a `sync.RWMutex`-protected map. The
admin API modifies the map at runtime. No infrastructure dependency. The
`ContentLibrary` interface decouples all handler code from the backing store —
swapping to PostgreSQL requires one new struct satisfying the interface; zero
handler changes.

## Admin API Demo

Start the auth server in one terminal:

```bash
cd backend/auth && go run main.go
```

Open a second terminal and run these steps one at a time.

**Step 1: Get a token**

```bash
curl -s -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test123"}'
```

Expected: JSON with `token` and `stream_url` fields. Copy the token value for all steps below.

**Step 2: List tracks**

```bash
curl -i -H "Authorization: Bearer PASTE_TOKEN_HERE" \
  http://localhost:8080/admin/tracks
```

Expected: 200 OK, JSON array with the lofi seed track.

**Step 3: Add a track**

```bash
curl -i -X POST \
  -H "Authorization: Bearer PASTE_TOKEN_HERE" \
  -H "Content-Type: application/json" \
  -d '{"id":"jazz","name":"Jazz Vibes","segment_path":"segments/jazz/","is_active":true}' \
  http://localhost:8080/admin/tracks
```

Expected: 201 Created, no body.

**Step 4: List tracks again**

```bash
curl -i -H "Authorization: Bearer PASTE_TOKEN_HERE" \
  http://localhost:8080/admin/tracks
```

Expected: 200 OK, both lofi and jazz tracks present.

**Step 5: Soft delete lofi**

```bash
curl -i -X DELETE \
  -H "Authorization: Bearer PASTE_TOKEN_HERE" \
  http://localhost:8080/admin/tracks/lofi
```

Expected: 204 No Content, no body.

**Step 6: Verify soft delete**

```bash
curl -i -H "Authorization: Bearer PASTE_TOKEN_HERE" \
  http://localhost:8080/admin/tracks
```

Expected: 200 OK, lofi shows `is_active` false, jazz still shows `is_active` true.

**Step 7: Delete nonexistent track**

```bash
curl -i -X DELETE \
  -H "Authorization: Bearer PASTE_TOKEN_HERE" \
  http://localhost:8080/admin/tracks/nonexistent
```

Expected: 404 Not Found.

**Step 8: Hit admin route with no token**

```bash
curl -i http://localhost:8080/admin/tracks
```

Expected: 401 Unauthorized.

**Step 9: Hit health with no token**

```bash
curl -i http://localhost:8080/health
```

Expected: 200 OK with body `ok`.

## Prerequisites

- Go 1.21+
- Node.js 18+
- Cloudflare account (free plan) with an R2 bucket named `lofi-stream`
- Wrangler CLI: `npm install -g wrangler`

## Cloudflare R2 setup

Full walkthrough in `CLOUDFLARE_SETUP.md`. Summary:

1. Create R2 bucket `lofi-stream` in the Cloudflare dashboard
2. Create an R2 API token (Object Read and Write, scoped to `lofi-stream`).
   Save the Access Key ID and Secret Access Key — shown once only.
3. Upload the pre-generated segments (one-time):

The segments directory contains a 2-minute lofi track split into 14 × ~10s AAC segments using FFmpeg:

```bash
ffmpeg -i lofi.mp3 -c:a aac -b:a 128k -hls_time 10 \
  -hls_list_size 0 segments/lofi/playlist.m3u8
```

Segments were pre-generated once and committed to the repository.

```bash
wrangler r2 object put lofi-stream/playlist.m3u8 \
  --file segments/lofi/playlist.m3u8 \
  --content-type application/vnd.apple.mpegurl

for i in $(seq -w 0 13); do
  wrangler r2 object put lofi-stream/seg0${i}.aac \
    --file segments/lofi/seg0${i}.aac \
    --content-type audio/aac
done
```

The segments directory contains a 2-minute lofi track split into 14 × ~10s
AAC segments by FFmpeg. Once uploaded they never change.

## Running locally

Create `backend/auth/.env`:

```
JWT_SECRET=dev-secret-change-in-prod
CF_ACCOUNT_ID=your-cloudflare-account-id
CF_ACCESS_KEY_ID=your-r2-access-key-id
CF_SECRET_ACCESS_KEY=your-r2-secret-access-key
CF_BUCKET_NAME=lofi-stream
TOKEN_EXPIRY_HOURS=4                # token lifetime — configurable per session requirement
TRACKS_FILE=tracks.json
```

Start the auth service:

```bash
cd backend/auth && go run main.go
# → localhost:8080
```

Start the frontend (separate terminal):

```bash
cd frontend && npm install && npm run dev
# → localhost:5173
```

## Using the app

Open `http://localhost:5173`. Log in with:

```
username: test
password: test123
```

Click **Play**. Audio streams from Cloudflare R2. The playlist loops
automatically when all 14 segments finish. Click **Pause** / **Play**
to toggle. A page refresh requires re-authentication — the token is held
only in React state, never in `localStorage`.

## Admin API

All `/admin/*` routes require a valid JWT.

**Get a token:**

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test123"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
```

**List tracks:**

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/admin/tracks
```

**Add a track:**

```bash
curl -s -X POST http://localhost:8080/admin/tracks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"jazz","name":"Jazz Vibes","segment_path":"segments/jazz/","is_active":true}'
```

**Deactivate a track (soft delete — sets `is_active=false`, does not remove):**

```bash
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/admin/tracks/jazz
```

## Project structure

```
├── segments/lofi/               Pre-generated HLS segments (upload to R2 once)
│   ├── playlist.m3u8
│   └── seg000.aac … seg013.aac
│
├── backend/auth/                Only backend service (localhost:8080)
│   ├── main.go                  Startup, env, AWS client wiring, routes
│   ├── handlers/
│   │   ├── auth.go              POST /auth/token — JWT + presigned playlist data URL
│   │   └── library.go           ContentLibrary interface, InMemoryLibrary, admin handlers
│   ├── middleware/
│   │   └── jwt.go               Bearer token validation (HS256, algorithm check)
│   ├── tracks.json              Seed content library
│   └── .env                     Credentials (not committed)
│
└── frontend/                    React/Vite app (localhost:5173)
    └── src/
        ├── App.jsx              Two-state machine: unauthenticated / authenticated
        └── components/
            ├── Login.jsx        Credential form, POST /auth/token
            └── Player.jsx       HLS.js init, play/pause toggle, ended-event loop
```

## Annotated structure

```
worktrial/
│
├── segments/lofi/                 ← pre-generated by FFmpeg once; never changes
│   ├── playlist.m3u8              ← static HLS index; EXT-X-ENDLIST makes it
│   │                                 finite — client-side loop handles replay
│   └── seg000.aac … seg013.aac   ← ~10s AAC segments; uploaded to R2 via
│                                     Wrangler once; auth service rewrites this
│                                     playlist per-login with presigned URLs
│
├── backend/auth/                  ← the only Go process; handles tokens only,
│   │                                 never proxies audio bytes
│   ├── main.go                    ← env validation, AWS S3 client init,
│   │                                 Chi router + CORS middleware + route wiring
│   ├── handlers/
│   │   ├── auth.go                ← POST /auth/token: validates credentials,
│   │   │                             signs JWT, calls GetObject(playlist.m3u8),
│   │   │                             presigns 14 segment URLs, returns data URL
│   │   └── library.go             ← ContentLibrary interface + InMemoryLibrary
│   │                                 (sync.RWMutex map) + LibraryHandler
│   │                                 (GET/POST/DELETE /admin/tracks)
│   ├── middleware/
│   │   └── jwt.go                 ← HS256 Bearer token validation; checks
│   │                                 signing method before returning secret
│   │                                 (blocks algorithm-confusion attacks)
│   ├── tracks.json                ← seed data loaded at startup into memory
│   ├── go.mod                     ← module: auth; deps: chi, jwt/v5,
│   │                                 aws-sdk-go-v2/s3, aws-sdk-go-v2/config
│   └── .env                       ← CF_ACCOUNT_ID, CF_ACCESS_KEY_ID,
│                                     CF_SECRET_ACCESS_KEY, JWT_SECRET, etc.
│
└── frontend/src/                  ← React/Vite; no SSR, no router,
    │                                 no state manager
    ├── App.jsx                    ← two-state machine: !token → <Login>,
    │                                 token set → <Player streamUrl={streamUrl}>
    └── components/
        ├── Login.jsx              ← controlled form; POST /auth/token;
        │                             token in React state only, never localStorage
        └── Player.jsx             ← useEffect mounts HLS.js on streamUrl;
                                      ended event calls hls.loadSource() for
                                      infinite loop; play() catch guard handles
                                      browser autoplay policy rejection
```

## Known limitations and production path

| Limitation | Production path |
|---|---|
| Presigned URLs expose the R2 S3 API domain (`*.r2.cloudflarestorage.com`) | WAF HMAC token auth via `is_timed_hmac_valid_v0()` on Cloudflare Pro ($20/mo) + custom domain. One env var change in auth service, zero client changes. |
| Single hardcoded user (`test`/`test123`) | Real user store with hashed passwords. The `users` map in `handlers/auth.go` is the only change point. |
| In-memory content library — changes lost on restart | Swap `InMemoryLibrary` for a PostgreSQL-backed implementation of `ContentLibrary`. Zero handler changes required. |
| `stream_url` is a ~8KB data URL (14 presigned segments base64-encoded) | Acceptable for demo. Grows linearly with segment count. WAF HMAC production path eliminates per-segment presigning entirely. |
