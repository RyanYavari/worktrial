# Build Tasks

## Status
- [x] Task 1:  Auth service skeleton (Chi router, health check, :8080)
- [x] Task 2:  POST /auth/token (hardcoded user, JWT + S3 presigned URL)
- [x] Task 3:  JWT middleware
- [x] Task 4:  tracks.json + InMemoryLibrary + ContentLibrary interface
- [x] Task 5:  Admin CRUD API (GET/POST/DELETE /admin/tracks)
- [x] Task 6:  Test full auth flow with curl
- [x] Task 7:  React login component
- [ ] Task 8:  React player (HLS.js, play/pause, infinite loop)
- [ ] Task 9:  Full end to end test in browser
- [ ] Task 10: KISS audit
      Claude reviews every file written during this project and
      flags anything that can be simplified. Checks for:
      - Dead code or unused imports
      - Abstractions that were not asked for
      - Logic that can be expressed in fewer lines
      - Anything added speculatively beyond what Kristian asked
      Goal: every file should do exactly what was asked, nothing more.
- [ ] Task 11: README + demo prep

## Rules
- Do not start the next task until the current one is tested and committed.
- Mark a task done only after the verify steps pass.
- One task at a time. No skipping ahead.