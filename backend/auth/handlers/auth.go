package handlers

import (
	"context"
	"encoding/json"
	"net/http"
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
	PresignClient *s3.PresignClient
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
// Cloudflare R2 presigned URL. The presigned URL is handed to HLS.js; segments
// are fetched directly from R2 — no Authorization header, no proxy.
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

	// Generate a Cloudflare R2 presigned URL for the HLS playlist.
	// R2 validates the embedded SigV4 signature at the edge — no Authorization
	// header is needed on HLS.js segment requests.
	streamURL, err := generatePresignedURL(r.Context(), h.PresignClient, h.Bucket, "playlist.m3u8", h.TokenExpiry)
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
