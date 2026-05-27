# Auth Service Context

## Purpose
Single backend service. Issues JWTs for API access. Generates S3 presigned URLs for Cloudflare R2 stream access. 
Manages content library. Exposes admin API. Never touches audio bytes.

## Port: 8080

## Critical: No Stream Service
Cloudflare R2 serves all audio segments. This auth service is 
the only Go service in the entire system.

## Routes
POST   /auth/token          → public, returns JWT + Cloudflare signed URL
POST   /auth/refresh        → public, refresh expired JWT
GET    /admin/tracks        → JWT protected
POST   /admin/tracks        → JWT protected
DELETE /admin/tracks/:id    → JWT protected (soft delete)
GET    /health              → public

## POST /auth/token Response
{
  "token": "eyJ...",
  "stream_url": "https://{CF_ACCOUNT_ID}.r2.cloudflarestorage.com/lofi-stream/playlist.m3u8?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=...&X-Amz-Expires=14400&X-Amz-Signature=..."
}

Both fields are required. Frontend uses token for API calls 
and stream_url directly with HLS.js.

## JWT
Library: github.com/golang-jwt/jwt/v5
Secret: JWT_SECRET env var
Expiry: TOKEN_EXPIRY_HOURS env var (default 4 hours)
Claims: user_id, exp, iat
Used for: API endpoints and admin routes only
NOT used for: Cloudflare segment access

## Hardcoded User
var users = map[string]string{"test": "test123"}
No database for user auth. Kristian confirmed this is fine.

## S3 Presigned URL Generation
Used for Cloudflare R2 stream access. AWS SDK v2 pointed at
R2 S3-compatible API endpoint.

Required dependencies:
github.com/aws/aws-sdk-go-v2/service/s3
github.com/aws/aws-sdk-go-v2/config

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

Called on every POST /auth/token. Returns real Cloudflare R2
presigned URL. Not a stub. Not a placeholder.
R2 endpoint: https://{CF_ACCOUNT_ID}.r2.cloudflarestorage.com

## R2 Checksum Compatibility Fix
AWS SDK v2 v1.73.0+ adds X-Amz-Checksum-Mode=ENABLED to 
presigned URLs by default. Cloudflare R2 does not support 
this parameter and rejects requests with InvalidArgument.

Fix applied in config.LoadDefaultConfig:
  config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired)
  config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired)

Do NOT remove these two lines. Removing them breaks R2 
presigned URL generation.

## JWT Middleware
Auth service middleware reads Authorization: Bearer {token} header.
Query parameter pattern is NOT used here.
Query parameter is Cloudflare's concern, not this service's.

## Content Library
tracks.json loaded at startup into InMemoryLibrary.
No database. No Docker. No schema migrations.
Kristian confirmed: JSON is fine, persistence not necessary.

tracks.json structure:
[
  {
    "id": "lofi",
    "name": "Lofi Beats",
    "segment_path": "segments/lofi/",
    "is_active": true
  }
]

ContentLibrary interface is the contract all handlers depend on.
InMemoryLibrary implements it with sync.RWMutex.
Soft delete sets is_active = false, never removes from map.

## ContentLibrary Interface
type ContentLibrary interface {
    GetTrack(id string) (Track, error)
    ListTracks() ([]Track, error)
    AddTrack(track Track) error
    DeactivateTrack(id string) error
}

## Environment Variables
JWT_SECRET=dev-secret-change-in-prod
CF_ACCOUNT_ID=your-cloudflare-account-id
CF_ACCESS_KEY_ID=your-r2-access-key-id
CF_SECRET_ACCESS_KEY=your-r2-secret-access-key
CF_BUCKET_NAME=lofi-stream
TOKEN_EXPIRY_HOURS=4
TRACKS_FILE=tracks.json

## CORS
Allow-Origin: http://localhost:5173
Allow-Headers: Authorization, Content-Type
Handle OPTIONS preflight.
