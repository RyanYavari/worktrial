package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang-jwt/jwt/v5"
)

// Hardcoded test user per Kristian's instruction — no registration, no database.
var users = map[string]string{"test": "test123"}

// Handler holds dependencies injected at startup. All fields are set once in
// main.go and are read-only after that — no locking needed.
type Handler struct {
	S3Client      *s3.Client      // fetches playlist content from R2
	PresignClient *s3.PresignClient // generates per-segment presigned URLs
	JWTSecret     string
	Bucket        string
	TokenExpiry   time.Duration
}

// tokenRequest is the expected JSON body for POST /auth/token.
type tokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// tokenResponse is the JSON body returned by POST /auth/token.
// Both fields are required — token gates API calls, stream_url goes directly to HLS.js.
type tokenResponse struct {
	Token     string `json:"token"`
	StreamURL string `json:"stream_url"`
}

// Token handles POST /auth/token. On success it returns a signed JWT and a
// data URL containing a fully-rewritten M3U8 playlist. Every segment line is
// replaced with an individual R2 presigned URL so each segment request carries
// its own SigV4 signature — satisfying AD #4 (stream protected by presigned
// URLs). HLS.js loads the data URL directly; no Authorization header is used
// anywhere in the segment flow.
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	// Decode JSON credentials from the request body.
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate against the hardcoded users map. Constant-time comparison is not
	// required — this is a single-user demo with no meaningful attack surface.
	pass, ok := users[req.Username]
	if !ok || pass != req.Password {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Sign a JWT with user_id, iat, and exp using HS256 and JWT_SECRET.
	// Expiry is set to the same window as the presigned URL so both
	// credentials become invalid at the same time.
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id": req.Username,
		"iat":     now.Unix(),
		"exp":     now.Add(h.TokenExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Fetch the static playlist from R2, rewrite each segment line with an
	// individual presigned URL, and encode as a data URL. HLS.js loads the
	// data URL directly — no new endpoint required.
	streamURL, err := buildPresignedPlaylist(r.Context(), h.S3Client, h.PresignClient, h.Bucket, h.TokenExpiry)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return both credentials. Frontend stores token for API calls and passes
	// stream_url directly to hls.loadSource().
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResponse{Token: signed, StreamURL: streamURL})
}

// Refresh handles POST /auth/refresh.
// Not implemented for the demo — the 4-hour JWT window is sufficient per Kristian.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// buildPresignedPlaylist fetches the static M3U8 from R2, rewrites every segment
// filename line with an individually presigned URL, and returns the result as a
// base64 data URL. A data URL lets HLS.js call hls.loadSource() without a new
// endpoint. Segment presigned URLs share the same expiry as the JWT so both
// credentials expire together. M3U8 directive lines (starting with '#') and
// empty lines are passed through unchanged — all FFmpeg timing data is preserved.
func buildPresignedPlaylist(ctx context.Context, s3Client *s3.Client, presignClient *s3.PresignClient, bucket string, expiry time.Duration) (string, error) {
	// Fetch the static playlist from R2 using the same client configuration
	// (endpoint, credentials) used for presigning — no extra setup required.
	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("playlist.m3u8"),
	})
	if err != nil {
		return "", err
	}
	defer result.Body.Close()

	// Read the full playlist. At 14 segments the file is ~350 bytes.
	raw, err := io.ReadAll(result.Body)
	if err != nil {
		return "", err
	}

	// Walk each line. Directive lines ('#') and blank lines are kept verbatim.
	// Segment filename lines are replaced with presigned URLs so R2 validates
	// the SigV4 signature before serving each audio segment.
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		segURL, err := generatePresignedURL(ctx, presignClient, bucket, trimmed, expiry)
		if err != nil {
			return "", err
		}
		lines[i] = segURL
	}

	// Encode the rewritten playlist as a base64 data URL. All segment lines
	// are now absolute presigned URLs, so the data URL base path is irrelevant
	// to HLS.js segment resolution.
	rewritten := strings.Join(lines, "\n")
	dataURL := "data:application/vnd.apple.mpegurl;base64," +
		base64.StdEncoding.EncodeToString([]byte(rewritten))
	return dataURL, nil
}

// generatePresignedURL returns an S3 presigned GET URL for an object in Cloudflare R2.
// Cloudflare R2 is S3-compatible — the AWS SDK v2 signs the request identically to S3,
// only the base endpoint differs. The signature is embedded in the URL as query
// parameters (SigV4), which R2 validates at the edge before serving the segment.
func generatePresignedURL(ctx context.Context, client *s3.PresignClient, bucket, key string, expiry time.Duration) (string, error) {
	req, err := client.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}
