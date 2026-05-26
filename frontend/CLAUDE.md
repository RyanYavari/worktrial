# Frontend Context

## Purpose
Login form + audio player. Two components. One state machine.

## Stack
React, JavaScript, Vite, HLS.js

## Application States
unauthenticated → login form visible, player hidden
authenticated   → login hidden, player visible

## Login Component
POST http://localhost:8080/auth/token
Body: {username, password}
Response: {token: string, stream_url: string}
Store both in React state. Pass stream_url to Player component.
Store JWT in React state (NOT localStorage).
On success: transition to authenticated state.
On failure: show error message.

## Player Component
On mount: initialize HLS.js with the stream_url returned 
from POST /auth/token. This URL points directly at Cloudflare R2.

HLS.js initialization:
const hls = new Hls()
hls.loadSource(streamUrl)  // streamUrl is the Cloudflare signed URL
hls.attachMedia(audioRef.current)

The stream_url already contains AWS SigV4 signature parameters 
as query parameters. HLS.js uses it directly. No Authorization 
header. No additional token handling needed for segments.

Cloudflare R2 validates the SigV4 signature at the edge before 
serving segments.

Infinite loop: audioElement ended event → hls.loadSource(streamUrl) → play()
Play/Pause toggle: audioElement.play() / audioElement.pause()
Single button, text changes based on isPlaying state.

## What To Build
- Login.jsx: form with username/password inputs, submit button
- Player.jsx: play/pause button, minimal UI
- App.jsx: state machine switching between Login and Player

## What NOT To Build
- No localStorage or sessionStorage
- No user registration
- No token refresh UI (4 hour expiry, not needed for demo)
- No volume controls, progress bar, or other audio UI
- No routing library
- No CSS framework (inline styles or simple CSS is fine)
- KISS: minimum UI that demonstrates the full auth + stream flow

## HLS.js Import
import Hls from 'hls.js'
Already installed: npm install hls.js done at project setup.

## Audio Element
Use useRef<HTMLAudioElement>(null) for the audio element.
Do not use React state for the audio element itself.

## Stream URL
Do NOT construct the playlist URL manually.
Use stream_url exactly as returned from POST /auth/token.
Points at Cloudflare R2 S3 API endpoint. Already contains AWS 
SigV4 signature parameters. Do not modify the URL in any way.
Pass directly to hls.loadSource(streamUrl).