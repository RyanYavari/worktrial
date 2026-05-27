# Lofi Stream

Authenticated lofi audio streaming demo. A Go/Chi auth service issues JWTs and
generates per-segment Cloudflare R2 presigned URLs on login. The React frontend
uses HLS.js to stream directly from R2 with no audio data passing through the
auth service. Designed to scale to 100k concurrent users via CDN edge delivery.

## Architecture

```
Browser → POST /auth/token → JWT + data URL (rewritten M3U8)
        → HLS.js loads data URL → fetches segments from Cloudflare R2
        → R2 validates SigV4 signature on each segment request
```

Key decisions:

- **HLS** — pre-generated AAC segments are CDN-cacheable static files
- **Cloudflare R2** — zero egress cost, edge delivery; replaces any local stream server
- **Per-segment presigned URLs** — on login, the static `playlist.m3u8` is fetched from R2
  and each segment filename is replaced with an individual SigV4 presigned URL; the
  rewritten playlist is returned as a base64 data URL in `stream_url`
- **JWT** — gates the auth service API (`/admin/*`); segment requests use presigned
  URL query parameters, not Authorization headers
- **Content library** — `tracks.json` loaded at startup into `InMemoryLibrary`;
  `ContentLibrary` interface allows swapping to PostgreSQL without touching handlers

## Prerequisites

- Go 1.21+
- Node.js 18+
- A Cloudflare account with an R2 bucket named `lofi-stream`
- Wrangler CLI (`npm install -g wrangler`) for uploading segments

See `CLOUDFLARE_SETUP.md` for the full R2 setup walkthrough.

## Setup

Create `backend/auth/.env`:

```
JWT_SECRET=dev-secret-change-in-prod
CF_ACCOUNT_ID=your-cloudflare-account-id
CF_ACCESS_KEY_ID=your-r2-access-key-id
CF_SECRET_ACCESS_KEY=your-r2-secret-access-key
CF_BUCKET_NAME=lofi-stream
TOKEN_EXPIRY_HOURS=4
TRACKS_FILE=tracks.json
```

Upload HLS segments to R2 (one-time):

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

## Running locally

Start the auth service:

```bash
cd backend/auth && go run main.go
# Listening on localhost:8080
```

Start the frontend (separate terminal):

```bash
cd frontend && npm install && npm run dev
# Listening on localhost:5173
```

## Using the app

Open `http://localhost:5173`. Log in with:

```
username: test
password: test123
```

Click **Play** to start the stream. Audio loops automatically when the playlist ends.

## Admin API

All admin routes require a valid JWT in the `Authorization: Bearer` header.

**Get a token:**

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test123"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
```

**List tracks:**

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/admin/tracks
```

**Add a track:**

```bash
curl -X POST http://localhost:8080/admin/tracks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"jazz","name":"Jazz Vibes","segment_path":"segments/jazz/","is_active":true}'
```

**Deactivate a track (soft delete):**

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/admin/tracks/jazz
```

## Project structure

```
├── segments/lofi/          # Pre-generated HLS segments (upload to R2)
│   ├── playlist.m3u8
│   └── seg000.aac … seg013.aac
├── backend/auth/           # Only backend service (localhost:8080)
│   ├── main.go             # Startup, wiring, routes
│   ├── handlers/
│   │   ├── auth.go         # POST /auth/token — JWT + presigned playlist
│   │   └── library.go      # ContentLibrary interface, InMemoryLibrary, admin handlers
│   ├── middleware/
│   │   └── jwt.go          # Bearer token validation
│   ├── tracks.json         # Seed content library
│   └── .env                # Credentials (not committed)
└── frontend/               # React/Vite app (localhost:5173)
    └── src/
        ├── App.jsx          # Auth state machine
        └── components/
            ├── Login.jsx    # Credential form
            └── Player.jsx   # HLS.js player, play/pause, infinite loop
```
