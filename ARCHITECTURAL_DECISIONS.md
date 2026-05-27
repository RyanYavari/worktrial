# Complete Architectural Decisions Document

## Lofi Stream Work Trial

---

# AD #1: Streaming Protocol

**Decision**
Use HLS (HTTP Live Streaming) for audio delivery.

**Context**
Streaming continuous lofi audio to 100,000 concurrent users. Core requirement is scalability to that user count. Latency is not a constraint since this is pre-recorded audio, not live radio.

**Why HLS**
HLS splits audio into small static segments served over plain HTTP. Static files are CDN-cacheable. At 100k users the CDN serves cached segments and the origin server handles only auth and playlist requests, which are lightweight and infrequent. This is the only approach that directly solves the 100k user scalability requirement without per-user server resources.

**Options Considered**

Chunked HTTP: Simple to implement but cannot be CDN-cached. Every user holds a persistent connection to the origin server. Does not scale beyond a few thousand users without massive infrastructure.

WebRTC: Sub-500ms latency but requires SFU infrastructure and is designed for bidirectional communication. Overkill for a listening-only use case with no latency requirement.

Low-Latency HLS: Better for live audio where latency matters. For pre-recorded lofi content latency is irrelevant. LL-HLS adds significant implementation complexity with no benefit for this specific task.

**Pros**

- CDN-cacheable static segments solve the 100k user problem
- Standard protocol with excellent tooling
- HLS.js handles all browser playback complexity
- Origin server is completely out of the audio byte path with CDN

**Cons**

- Slightly higher latency than WebRTC or chunked HTTP
- Requires FFmpeg pre-processing step

**Production Evolution**
For the real ATC live radio product, evolving to LL-HLS reduces latency to 1-3 seconds while preserving the same CDN scalability architecture. No structural changes required.

---

# 

```
# AD #2: CDN and Storage

**Decision**
Cloudflare R2 for audio segment storage and delivery. Demo
connects to live Cloudflare R2 as a real external service.
There is no local stream service. Cloudflare replaces it
entirely in demo and production alike.

**Context**
HLS delivers static audio segments over HTTP. At 100k concurrent
users these segments must be cached at the edge. A CDN is
required. The core problem is bandwidth: 128kbps audio at 100k
users is 1.6GB per second. No single Go server handles that.

**Why Cloudflare R2**
Zero egress fees is the decisive factor. AWS CloudFront costs
approximately $2,000 per full stream cycle at 100k users.
Cloudflare R2 costs $0. Bunny.net is cheaper than AWS but
Cloudflare free egress makes the difference irrelevant. R2
uses the S3-compatible API so the AWS SDK v2 works out of the box.

**Demo Path: S3 Presigned URLs**
Auth service generates S3 presigned URLs via AWS SDK v2 pointed
at the R2 S3-compatible API endpoint:
https://{CF_ACCOUNT_ID}.r2.cloudflarestorage.com

func generatePresignedURL(ctx context.Context, client *s3.PresignClient,
bucket, key string, expiry time.Duration) (string, error) {
    req, err := client.PresignGetObject(ctx, &s3.GetObjectInput{
        Bucket: aws.String(bucket),
        Key:    aws.String(key),
    }, s3.WithPresignExpires(expiry))
    if err != nil {
        return "", err
    }
    return req.URL, nil
}

Required Go dependencies:
github.com/aws/aws-sdk-go-v2/service/s3
github.com/aws/aws-sdk-go-v2/config

**Production Path: WAF HMAC Token Auth**
Requires Cloudflare Pro plan ($20/month) and a custom domain.
WAF validates HMAC tokens at the edge via is_timed_hmac_valid_v0().
Confirmed blocker: not available on free tier, not available via
r2.dev subdomain. S3 presigned URLs are the correct demo substitute
with identical security properties from the client's perspective.
One environment variable change to swap. Zero application code changes.

**Known Limitation**
S3 presigned URLs expose the R2 S3 API domain rather than a
clean URL. Acceptable for demo. Production path is WAF HMAC
token auth behind a custom domain on Cloudflare Pro plan.
WAF HMAC requires Pro plan ($20/month) and a custom domain,
confirmed via Cloudflare docs. Not viable for demo scope.

**Demo Setup**
1. Create Cloudflare account at cloudflare.com (free plan)
2. Create R2 bucket named lofi-stream
3. Enable r2.dev public subdomain on the bucket
4. Create R2 API token with Object Read and Write permissions.
   Save Access Key ID and Secret Access Key immediately.
   They are shown once only.
5. Upload segments via Wrangler:
   wrangler r2 object put lofi-stream/playlist.m3u8 \
     --file segments/lofi/playlist.m3u8
   (repeat for all 14 .aac segments)
6. Set environment variables in backend/auth/.env:
   CF_ACCOUNT_ID, CF_ACCESS_KEY_ID, CF_SECRET_ACCESS_KEY,
   CF_BUCKET_NAME

**Pros**
- Zero egress cost at any scale
- S3 presigned URLs work on free Cloudflare plan
- R2 validates SigV4 natively, no Worker needed
- Production path is one environment variable change

**Cons**
- Presigned URLs expose the R2 S3 API domain, not a clean URL
- Production WAF HMAC requires paid Cloudflare plan
- Demo requires live internet connection to reach Cloudflare R2
```

---

# 

# 

# AD #3: Service Architecture

**Decision**
Single Go/Chi auth service. Cloudflare R2 replaces the stream
service entirely. There is no backend/stream/ directory.

**Context**
The system has two workloads: auth token issuance and audio
segment delivery. These have different scaling requirements.
The solution is not two Go services. It is one Go service for
auth and Cloudflare's global CDN for delivery.

**Why One Service**
Cloudflare R2 is a better stream service than anything a Go
server could be in this use case. 300+ edge nodes, zero egress
cost, 1.6GB/second at 100k users handled trivially. A Go stream
service sitting in front of it adds a failure point with no
benefit. The auth service issues tokens. Cloudflare serves bytes.
These are the right tools for each job.

**Local Configuration**
Auth service:   localhost:8080
Cloudflare R2:  live external service (segments uploaded via Wrangler)
React frontend: localhost:5173

NO localhost:8081. NO local stream service.

**Options Considered**

Two Go services (auth + stream): Stream service serves segments
locally from filesystem. Correctly separates concerns but adds
a local process that disappears entirely in production. Forces
a code change rather than a configuration change to go live.
Does not reflect the real production architecture. Rejected.

One Go auth service + Cloudflare R2 (chosen): Auth service
handles all token operations. Cloudflare handles all audio
delivery in demo and production alike. Production and demo
use identical architecture. Deployment is environment variable
changes only.

**Pros**

- One service to start locally
- No CORS complexity between auth and stream services
- Demo and production use identical architecture
- Cloudflare handles scale no single Go server could match

**Cons**

- Requires Cloudflare account and R2 bucket setup before coding
- External dependency means internet connection required for demo

---

# 

# AD #4: Authentication Pattern

**Decision**
JWT for API and admin endpoints. S3 presigned URLs via AWS SDK v2
pointed at Cloudflare R2 for stream access. Two token types,
one response.

**Context**
Kristian specified the entire process must be protected and that
the client asks the server for a token that is valid for the
stream. The system has two distinct access patterns: API requests
using headers, and HLS segment requests that HLS.js fetches
automatically.

**Demo Implementation**
Two token types. JWT for API and admin endpoints via Authorization
header. S3 presigned URL for stream access returned directly from
POST /auth/token.

```
POST /auth/token          → public, returns JWT + presigned stream URL
GET  /admin/tracks        → Authorization: Bearer JWT
POST /admin/tracks        → Authorization: Bearer JWT
DELETE /admin/tracks/:id  → Authorization: Bearer JWT
```

**Why S3 Presigned URL for Stream**
HLS.js fetches playlist and segment URLs automatically. It cannot
add Authorization headers to segment requests without breaking CDN
caching. The auth service generates an S3 presigned URL for the R2
playlist using AWS SDK v2. The presigned URL contains AWS SigV4
signature parameters as query params. HLS.js uses the URL directly.
R2 validates the SigV4 signature at the edge. No custom token
handling needed in the frontend.

**Implementation**

Required Go dependencies:
[github.com/aws/aws-sdk-go-v2/service/s3](http://github.com/aws/aws-sdk-go-v2/service/s3)[github.com/aws/aws-sdk-go-v2/config](http://github.com/aws/aws-sdk-go-v2/config)

```go
func generatePresignedURL(ctx context.Context, client *s3.PresignClient,
bucket, key string, expiry time.Duration) (string, error) {
    req, err := client.PresignGetObject(ctx, &s3.GetObjectInput{
        Bucket: aws.String(bucket),
        Key:    aws.String(key),
    }, s3.WithPresignExpires(expiry))
    if err != nil {
        return "", err
    }
    return req.URL, nil
}
```

Called on every POST /auth/token. Returns a real Cloudflare R2
presigned URL. Not a stub. Not a placeholder.

POST /auth/token response:

```json
{
  "token": "eyJ...",
  "stream_url": "https://{ACCOUNT_ID}.r2.cloudflarestorage.com/lofi-stream/playlist.m3u8?X-Amz-Algorithm=..."
}
```

**Known Limitation**
S3 presigned URLs expose the R2 S3 API domain
(`<ACCOUNT_ID>.r2.cloudflarestorage.com`) rather than a clean
branded URL. Acceptable for demo. Production path is WAF HMAC
token auth behind a custom domain on a Cloudflare Pro plan or
above, which gives edge-level validation with a clean URL.

**Production Path: WAF HMAC Signed URLs**
Requires Cloudflare Pro plan ($20/month) and a custom domain
pointed at Cloudflare. Auth service swaps to generating HMAC
signed URLs compatible with Cloudflare's is_timed_hmac_valid_v0()
WAF function. Cloudflare validates at the edge with no custom
Worker code. The client receives a signed URL and passes it
directly to HLS.js. Zero changes to client code. One environment
variable change in the auth service.

**Options Considered**

S3 presigned URLs via R2 S3-compatible API (chosen): AWS SigV4
signature embedded as query params. R2 validates at edge. Works
on free Cloudflare plan. Exposes S3 API domain rather than clean
URL. Correct demo pattern.

WAF HMAC token auth: Correct production pattern. Requires
Cloudflare Pro plan ($20/month) and a custom domain pointed at
Cloudflare. Both blockers confirmed via Cloudflare docs. Not
viable for demo scope.

Custom HMAC function: Rejected. A hand-rolled HMAC function in
the auth service has no validator on the other end unless a
Cloudflare Worker is written to check it. Adding a Worker is
scope creep. S3 presigned URLs give the same security with R2
doing the validation natively.

**Pros**

- S3 presigned URLs work on free Cloudflare plan
- R2 validates SigV4 signature natively, no Worker needed
- HLS.js uses stream_url directly with no custom handling
- Production path to WAF HMAC is one environment variable change
- Zero client code changes required to move to production

**Cons**

- Presigned URLs expose the R2 S3 API domain, not a clean URL
- Production WAF HMAC requires paid Cloudflare plan
- AWS SDK v2 dependency added to auth service

---

# AD #5: Seamless Looping Strategy

**Decision**
Pre-generate HLS segments using FFmpeg. Serve the static M3U8 playlist file directly from the filesystem. Handle infinite looping client-side in React. Expose play and pause controls to the user.

**Context**
Kristian specified: use a small audio file, loop it infinitely, no endpoint for the playlist, play and pause feature required. The stream must be seamless with no audible stitching artifacts.

**What Was Built**

FFmpeg processed lofi.mp3 (2 minutes 10 seconds) into 14 AAC segments of approximately 10 seconds each:

```
segments/lofi/
├── playlist.m3u8
├── seg000.aac through seg013.aac
```

Command:

```bash
ffmpeg -i lofi.mp3 \
  -c:a aac \
  -b:a 128k \
  -hls_time 10 \
  -hls_list_size 0 \
  -hls_segment_filename 'segments/lofi/seg%03d.aac' \
  segments/lofi/playlist.m3u8
```

**Why No Server Endpoint for Playlist**

Kristian explicitly said no endpoint. A static M3U8 file committed to the repository is the correct implementation of that instruction. The stream service is a file server for this resource, not a controller.

**Why Segments Are Gapless**

FFmpeg with AAC codec places segment boundaries at clean audio frame boundaries. HLS.js transitions between segments the same way regardless of whether it is crossing from seg005 to seg006 or from seg013 back to seg000. The loop is gapless because the codec, not the looping logic, handles boundaries.

**Infinite Looping Without Endpoint**

The static playlist has an EXT-X-ENDLIST tag making it technically finite. The React frontend handles the loop by reloading the HLS source when the audio element fires its ended event:

```tsx
audioElement.addEventListener('ended', () => {
    hls.loadSource(streamUrl)
    audioElement.play()
})
```

The stream service remains completely stateless. No server-side tracking of playback position.

**Play/Pause Implementation**

Single toggle button. Browser native audio API:

```tsx
const togglePlayback = () => {
    if (audioElement.paused) {
        audioElement.play()
        setIsPlaying(true)
    } else {
        audioElement.pause()
        setIsPlaying(false)
    }
}
```

HLS.js continues buffering in the background while paused. Resume is immediate from buffered position.

**Options Considered**

Server-side infinite playlist generation: Go handler generates M3U8 with no EXT-X-ENDLIST cycling through segments. Rejected because Kristian said no endpoint and it adds server-side logic for something the client handles trivially.

Pre-stitch into one long file: FFmpeg concatenates the file 50 times. Rejected because it creates a large binary in the repo with no playback improvement over the segments approach.

Static playlist with client-side loop (chosen): Matches Kristian's instruction exactly, stateless stream service, gapless by construction.

**Pros**

- Matches Kristian's exact instruction
- Stateless stream service
- Gapless by construction from FFmpeg AAC encoding
- Play/pause handled natively by browser

**Cons**

- Brief HLS.js reinitialization on loop, under 500ms, inaudible

---

# AD #6: Browser HLS Playback Library

**Decision**
HLS.js as the browser-side HLS playback library.

**Context**
HLS is not natively supported in Chrome, Firefox, or Edge. Only Safari supports it natively. A JavaScript library is required to parse M3U8 playlists and feed segments to the browser audio pipeline.

**Why HLS.js**

HLS.js does exactly one thing: play HLS streams in browsers that do not natively support it. The API is three lines:

```jsx
const hls = new Hls()
hls.loadSource(streamUrl)
hls.attachMedia(audioElement)
```

It handles playlist fetching, segment buffering, and error events. No streaming logic needed in React.

**Options Considered**

Video.js plus HLS plugin: Full-featured media player with UI components. Designed primarily for video. Overkill for audio-only streaming.

Shaka Player: Google's adaptive streaming player. Designed primarily for MPEG-DASH. HLS support is secondary. Heavier than HLS.js.

Native HTML5 Audio: Works only in Safari. Not viable for a cross-browser demo.

HLS.js (chosen): Purpose-built, 28kb gzipped, industry standard, active maintenance.

**Pros**

- Purpose-built for HLS
- 28kb gzipped, minimal bundle impact
- Handles all segment fetching automatically
- Error events enable token expiry detection

**Cons**

- One npm dependency
- Requires understanding error event model

---

# 

## AD #7: Content Library Management

**Decision**
JSON file loaded into memory at service startup. In-memory map is the runtime store. CRUD admin API modifies the in-memory map at runtime. Changes do not persist across restarts. This is acceptable for demo scope per Kristian's explicit confirmation that persistence is not necessary and minimum viable solution is fine.

**Context**
The content library is dynamic. Tracks must be addable and removable at runtime without code changes or service restarts. Kristian confirmed JSON is acceptable and persistence is not required for the demo.

**Options Considered**

**Option A: PostgreSQL + in-memory TTL cache**
Durable, queryable, ACID guarantees. Tracks survive restarts. Multiple service instances share the same source of truth. Soft delete with full audit trail.

Cons: Requires Docker. Schema migrations. Connection management. 2 to 3 hours of setup time for zero demo benefit. Kristian explicitly said persistence is not necessary.

**Option B: Redis**
In-memory database with optional persistence. Fast reads, pub/sub for cache invalidation across multiple service instances.

Cons: Additional infrastructure. Overkill for single-instance demo. Same setup overhead as PostgreSQL for this use case without the relational benefits.

**Option C: Hardcoded Go map**
Zero setup. Tracks defined as a Go map literal at compile time.

Cons: Not dynamic. Changing tracks requires a code change and recompile. Does not satisfy the dynamic content library requirement even for demo purposes.

**Option D: JSON file with in-memory runtime store (chosen)**
tracks.json loaded at startup into a Go map. Admin API modifies the in-memory map. The JSON file serves as the initial state and is human-readable. No infrastructure required. Satisfies the dynamic requirement because tracks can be added and removed via API at runtime. Persistence not required per Kristian.

**Why JSON File Over Alternatives**
PostgreSQL and Redis are correct for production but add significant setup overhead for zero demo benefit. A hardcoded Go map is not dynamic. A JSON file is the minimum viable implementation that satisfies all demo requirements: dynamic at runtime via admin API, human-readable initial state, no infrastructure dependencies, zero setup time.

**Implementation**

tracks.json initial state:

json

`[
  {
    "id": "lofi",
    "name": "Lofi Beats",
    "segment_path": "../../segments/lofi/",
    "is_active": true
  }
]`

ContentLibrary interface (handlers depend on this, not the implementation):

go

`type ContentLibrary interface {
    GetTrack(id string) (Track, error)
    ListTracks() ([]Track, error)
    AddTrack(track Track) error
    DeactivateTrack(id string) error
}

type Track struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    SegmentPath string `json:"segment_path"`
    IsActive    bool   `json:"is_active"`
}`

InMemoryLibrary implementation:

go

`type InMemoryLibrary struct {
    tracks map[string]Track
    mu     sync.RWMutex
}`

Admin API (all protected by JWT middleware):

`POST   /admin/tracks        Add a track at runtime
GET    /admin/tracks        List all tracks
DELETE /admin/tracks/:id    Soft delete (is_active = false)`

Soft delete pattern: tracks are deactivated not deleted. Users mid-stream on a deactivated track complete naturally. New requests for that track receive 404. This is the correct pattern regardless of backing store.

**Why Soft Delete Matters Even Without Persistence**
Hard deletion during an active demo could break a stream mid-presentation. Soft delete is the safe pattern and demonstrates correct production thinking even in the minimal implementation.

**Production Evolution**
The ContentLibrary interface isolates all handler code from the backing store. Swapping InMemoryLibrary for a PostgreSQL-backed implementation requires creating one new struct that satisfies the interface. Zero handler changes. This is documented and explainable to Kristian as the production path.

`Demo:       JSON file → InMemoryLibrary → ContentLibrary interface
Production: PostgreSQL + TTL cache → CachedLibrary → ContentLibrary interface`

**Pros**

- Zero infrastructure dependencies
- Dynamic at runtime via admin API
- Human-readable initial state in tracks.json
- ContentLibrary interface isolates handlers from implementation
- Soft delete protects active streams
- Production evolution path is clean and explainable

**Cons**

- Changes do not persist across restarts
- No audit trail
- Not suitable for multi-instance deployment without shared state

---

# 

# AD #8: Stream Authentication

**Decision**
S3 presigned URLs generated by the auth service via AWS SDK v2
pointed at the Cloudflare R2 S3-compatible API endpoint.
There is no local stream service. This is the token Kristian
described: client asks server for a token valid for the stream.
The presigned URL IS that token. It expires after TOKEN_EXPIRY_HOURS.

**How It Works**
POST /auth/token validates hardcoded credentials, then generates
two things. A JWT for admin route access, and an S3 presigned URL
for stream access. The presigned URL is the stream token. HLS.js
uses it directly. R2 validates the AWS SigV4 signature at the edge.

POST /auth/token response:
{
"token": "eyJ...",
"stream_url": "https://{CF_ACCOUNT_ID}.r2.cloudflarestorage.com/
lofi-stream/playlist.m3u8?X-Amz-Algorithm=..."
}

No credentials → no stream URL → cannot stream.
Expired URL → stream dies → must re-authenticate.
Token lifetime controlled by TOKEN_EXPIRY_HOURS env var.

**Why Two Tokens**
The stream token (presigned URL) and the API token (JWT) serve
different purposes. HLS.js cannot add Authorization headers to
segment requests without breaking CDN caching. The presigned URL
embeds auth as query params, which R2 validates natively. The JWT
protects the content library and admin API via Authorization header.
Two tokens, one authentication event, correct tool for each job.

**Why Not WAF HMAC**
WAF HMAC token auth (is_timed_hmac_valid_v0) requires Cloudflare
Pro plan ($20/month) and a custom domain. Confirmed via Cloudflare
docs. Not viable for demo scope. S3 presigned URLs provide identical
security from the client's perspective with R2 validating at the edge.
Production path is WAF HMAC. One environment variable change to swap.

**Pros**

- Works on free Cloudflare plan
- R2 validates SigV4 signature natively, no Worker needed
- HLS.js uses stream_url directly with no custom handling
- Token expiry enforced at edge by R2
- Production path to WAF HMAC is one environment variable change

**Cons**

- Presigned URLs expose R2 S3 API domain, not a clean branded URL
- Production WAF HMAC requires paid Cloudflare plan
- AWS SDK v2 dependency added to auth service

---

# AD #9: Project Structure

**Decision**
Single monorepo with three top-level directories: backend, frontend, and segments.

**Structure**

```
/
├── README.md
├── segments/
│   └── lofi/
│       ├── playlist.m3u8
│       ├── seg000.aac
│       └── ... (seg001 through seg013)
├── backend/
│   └── auth/
│       ├── main.go
│       ├── go.mod
│       ├── tracks.json
│       ├── .env
│       ├── handlers/
│       │   ├── auth.go
│       │   └── library.go
│       └── middleware/
│           └── jwt.go
└── frontend/
    ├── index.html
    ├── package.json
    ├── vite.config.ts
    └── src/
        ├── App.tsx
        └── components/
            ├── Login.tsx
            └── Player.tsx
```

**Why Monorepo**

One clone, one README, one demo. Kristian can clone the repo and run the full system in under 5 minutes. Separate repos require coordinating multiple clones and startup sequences. There is one Go service to start, one frontend to start, and one external service (Cloudflare R2) already running. Total startup is two commands.

**Pros**

- Single clone for the entire system
- Structure explains itself
- One README covers everything

**Cons**

- Single git history for all components
- Not how production microservices would be organized long-term
- backend/ contains only one service; the flat structure implies a second service that does not exist
    - This is worth saying out loud to Kristian. It shows I am aware the structure slightly over-primises. I could simplify to auth/ at root level but the monorepo pattern is cleaner for a demo handoff.