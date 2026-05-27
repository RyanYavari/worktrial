# Lofi Stream — Claude Code Context

## Project Overview
Authenticated lofi audio streaming system. Single Go/Chi backend service,
 React/Vite frontend. Demo runs locally. Designed to 
scale to 100k concurrent users via Cloudflare R2 + HLS.

## Current Build Target
Demo runs on local machine but connects to live Cloudflare R2 
for segment delivery. Cloudflare is a real external service 
connected to this project.

Auth service runs locally on localhost:8080.
React frontend runs locally on localhost:5173.
Cloudflare R2 stores HLS segments and serves them via CDN.
There is NO local stream service. Cloudflare replaces it entirely.

Full flow:
Client → POST localhost:8080/auth/token
       → returns JWT + Cloudflare signed URL
       → HLS.js hits Cloudflare directly for segments
       → Cloudflare validates AWS SigV4 signature at edge
       → segments served from R2 to client

## Architecture in One Sentence
Client logs in → gets JWT + S3 presigned URL → 
HLS.js hits Cloudflare R2 directly for segments → 
R2 validates AWS SigV4 signature at edge → audio plays with 
play/pause and client-side infinite loop.

## Services
- Auth service:   localhost:8080 (JWT issuance, S3 presigned URL 
                  generation, content library, admin API)
- Cloudflare R2:  stores HLS segments, serves via CDN globally
- Frontend:       localhost:5173 (React/Vite)

NO local stream service. Cloudflare replaces it entirely.

## Tech Stack
- Backend: Go, Chi router, JWT (golang-jwt/jwt/v5)
- Content Library: JSON file loaded at startup into in-memory map.
- Frontend: React, Vite, JavaScript, HLS.js
  No database. No Docker. Kristian confirmed persistence not required.
- Audio: HLS segments pre-generated with FFmpeg, stored in segments/lofi/

## Key Architectural Decisions
1. HLS for streaming — CDN-cacheable static segments
2. Cloudflare R2 + CDN — zero egress cost, edge delivery, 
   replaces local stream service entirely
3. Single auth service — Cloudflare handles all segment delivery,
   auth service only issues tokens
4. JWT for API auth + S3 presigned URLs for Cloudflare stream access
5. Static M3U8 in R2 — no endpoint, Kristian's explicit instruction
6. Client-side infinite loop — audioElement ended event reloads HLS source
7. JSON file + InMemoryLibrary — dynamic content library, 
   persistence not required per Kristian
8. ContentLibrary interface — implementation swappable without 
   handler changes

## Production Path (not implemented in demo)
Stream service replaced by Cloudflare R2. Auth service generates 
HMAC signed URLs. Zero application code changes required.
generateSignedURL() function already present in auth service.

## Core Values
- Keep it simple above everything. If it can be simpler, make it simpler.
- Surgical changes only. Touch nothing outside the task scope.
- No speculative features. Build exactly what was asked.
- Match existing patterns in every file.
- Comment all code. Every function, every block of logic.
  Comments explain why, not just what. Keep the comments brief, concise, but dont omit important details. 
  Relate comments to important architectural decisions if appropriate. 

## Common Commands
# Start auth service
cd backend/auth && go run main.go

# Start frontend
cd frontend && npm run dev

# Test auth endpoint
curl -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test123"}'

# Expected response
{
  "token": "eyJ...",
  "stream_url": "https://{CF_ACCOUNT_ID}.r2.cloudflarestorage.com/lofi-stream/playlist.m3u8?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=...&X-Amz-Expires=14400&X-Amz-Signature=..."
}

# Upload segments to R2 (run once during setup)
wrangler r2 object put {bucket}/playlist.m3u8 --file segments/lofi/playlist.m3u8
wrangler r2 object put {bucket}/ --file segments/lofi/ --recursive

## Project Structure
/
├── README.md
├── CLAUDE.md
├── segments/lofi/               ← pre-generated HLS segments
│   ├── playlist.m3u8            ← uploaded to Cloudflare R2
│   └── seg000.aac ... seg013.aac
├── backend/
│   └── auth/                    ← only backend service
│       ├── main.go
│       ├── go.mod
│       ├── tracks.json
│       ├── .env
│       ├── handlers/
│       │   ├── auth.go          ← JWT + S3 presigned URL generation
│       │   └── library.go       ← content library CRUD
│       └── middleware/
│           └── jwt.go
└── frontend/
    ├── package.json
    ├── vite.config.ts
    └── src/
        ├── App.jsx
        └── components/
            ├── Login.jsx
            └── Player.jsx

NO backend/stream/ directory. Cloudflare replaces it.

## Environment Variables
# backend/auth/.env
JWT_SECRET=dev-secret-change-in-prod
CF_ACCOUNT_ID=your-cloudflare-account-id
CF_ACCESS_KEY_ID=your-r2-access-key-id
CF_SECRET_ACCESS_KEY=your-r2-secret-access-key
CF_BUCKET_NAME=lofi-stream
TOKEN_EXPIRY_HOURS=4
TRACKS_FILE=tracks.json


## Hardcoded Test User
username: test
password: test123
(No registration flow. Kristian confirmed hardcoded user is fine.)

## Content Library
Tracks managed via admin API. Soft delete pattern (is_active = false).
ContentLibrary interface allows PostgreSQL ↔ InMemory swap without 
touching handler code.

## HLS Looping
Static playlist has EXT-X-ENDLIST. Frontend reloads HLS source on 
audioElement ended event. Stream service is a pure file server.
No server-side loop logic.

## Do Not
- Create a backend/stream/ service. Cloudflare replaces it.
- Use Authorization headers for HLS.js segment requests
- Add features Kristian did not ask for
- Implement local segment serving
- Add user registration or persistent session storage
- Generate dynamic playlist content anywhere

## Current Build Status
Always read TASKS.md at the start of every task to know what has been completed and what comes next.

