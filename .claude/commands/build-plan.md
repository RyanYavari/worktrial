# Build Tasks

## Status
- [ ] Task 1:  Auth service skeleton (Chi router, health check, :8080)
- [ ] Task 2:  POST /auth/token (hardcoded user, JWT + S3 presigned URL)
- [ ] Task 3:  JWT middleware
- [ ] Task 4:  tracks.json + InMemoryLibrary + ContentLibrary interface
- [ ] Task 5:  Admin CRUD API (GET/POST/DELETE /admin/tracks)
- [ ] Task 6:  Test full auth flow with curl
- [ ] Task 7:  React login component
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

## Command Loop (follow this for every task)
1. Run /build-plan — Claude proposes plan, no code written yet
2. Paste plan into this chat for review before approving
3. Approve in Claude Code — Claude builds the code
4. Run verify steps manually (curl or browser)
5. Run /done — Claude reviews, simplifies, marks task complete in TASKS.md, commits
6. Move to next task