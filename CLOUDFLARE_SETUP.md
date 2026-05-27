# Cloudflare Setup

## Steps to Complete Before Coding Auth Service

1. Create Cloudflare account at cloudflare.com (free plan)
2. Create R2 bucket named lofi-stream
3. Enable r2.dev public subdomain on the bucket
   (Bucket → Settings → Public Access → Enable r2.dev subdomain)
4. Create R2 API token:
   R2 Overview → Manage API Tokens → Create API Token
   Permissions: Object Read and Write
   Scope: lofi-stream bucket only
   Save Access Key ID and Secret Access Key immediately.
   They are shown once only.
5. Install Wrangler and log in:
   npm install -g wrangler
   wrangler login
6. Upload segments:
   wrangler r2 object put lofi-stream/playlist.m3u8 \
     --file segments/lofi/playlist.m3u8 \
     --content-type application/vnd.apple.mpegurl

   for i in $(seq -w 0 13); do
     wrangler r2 object put lofi-stream/seg0${i}.aac \
       --file segments/lofi/seg0${i}.aac \
       --content-type audio/aac
   done

7. Add to backend/auth/.env:
   CF_ACCOUNT_ID=your-cloudflare-account-id
   CF_ACCESS_KEY_ID=your-r2-access-key-id
   CF_SECRET_ACCESS_KEY=your-r2-secret-access-key
   CF_BUCKET_NAME=lofi-stream

## S3 API Endpoint Format
https://{CF_ACCOUNT_ID}.r2.cloudflarestorage.com

The auth service uses this endpoint with AWS SDK v2 to generate
presigned URLs. The presigned URL is what gets returned as
stream_url in POST /auth/token.

## Verify Upload
After uploading, confirm all 15 files appear in the Cloudflare
dashboard under your lofi-stream bucket:
- playlist.m3u8
- seg000.aac through seg013.aac

## Test Cloudflare Setup
Generate a presigned URL by hitting your auth service:

curl -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test123"}'

Take the stream_url from the response and test it directly:

curl "{stream_url}"

Should return 200 with M3U8 content.

## Production Path (not implemented in demo)
WAF HMAC token auth via is_timed_hmac_valid_v0().
Requires Cloudflare Pro plan ($20/month) and a custom domain.
Zero application code changes to swap. One environment variable
change in auth service.