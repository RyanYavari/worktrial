package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"

	"auth/handlers"
	"auth/middleware"
)

func main() {
	// CF credentials and JWT_SECRET must be in the environment before any handler
	// initialises the AWS S3 client or signs tokens. godotenv.Load is a no-op when
	// .env is absent, so production deployments using real env vars are unaffected.
	godotenv.Load()

	// Fail fast if any required credential is missing. The service cannot issue JWTs
	// or generate presigned URLs without these — failing at startup gives a clearer
	// error than a cryptic failure inside a handler.
	jwtSecret := requireEnv("JWT_SECRET")
	cfAccountID := requireEnv("CF_ACCOUNT_ID")
	cfAccessKeyID := requireEnv("CF_ACCESS_KEY_ID")
	cfSecretAccessKey := requireEnv("CF_SECRET_ACCESS_KEY")
	cfBucket := requireEnv("CF_BUCKET_NAME")
	tracksFile := requireEnv("TRACKS_FILE")

	// Parse token expiry from env. Defaults to 4 hours — sufficient for a demo session.
	// Presigned URL expiry is set to the same duration so both credentials expire together.
	tokenExpiry := 4 * time.Hour
	if h := os.Getenv("TOKEN_EXPIRY_HOURS"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 {
			tokenExpiry = time.Duration(n) * time.Hour
		}
	}

	// Load the content library from tracks.json at startup. Fail fast if the file
	// is missing or malformed — the admin API cannot serve content without it.
	lib, err := handlers.NewInMemoryLibrary(tracksFile)
	if err != nil {
		log.Fatalf("failed to load tracks: %v", err)
	}
	// Build the Cloudflare R2 S3-compatible endpoint from the account ID.
	// R2 accepts the same AWS SigV4 signing protocol as S3 — only the base
	// endpoint needs to be overridden; no other SDK changes are required.
	r2Endpoint := "https://" + cfAccountID + ".r2.cloudflarestorage.com"

	// Configure the AWS SDK with static R2 credentials.
	// Region "auto" is required by Cloudflare R2 — it does not use AWS regions.
	// AWS SDK v2 defaults to adding X-Amz-Checksum-Mode=ENABLED to presigned URLs.
	// Cloudflare R2 does not support this parameter and returns InvalidArgument,
	// causing every HLS.js segment request to fail. Setting both checksum options
	// to "when required" suppresses the header from the presigned URL entirely.
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("auto"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfAccessKeyID, cfSecretAccessKey, "",
		)),
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	// Create the S3 client pointed at R2. UsePathStyle produces URLs of the form
	// /{bucket}/{key}, matching the expected stream_url format in CLAUDE.md.
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(r2Endpoint)
		o.UsePathStyle = true
	})
	presignClient := s3.NewPresignClient(s3Client)

	// Wire auth handler with all dependencies resolved at startup. No global state —
	// the Handler struct is the single source of shared dependencies.
	h := &handlers.Handler{
		S3Client:      s3Client,
		PresignClient: presignClient,
		JWTSecret:     jwtSecret,
		Bucket:        cfBucket,
		TokenExpiry:   tokenExpiry,
	}

	// Wire library handler. Uses the ContentLibrary interface so the backing
	// store can be swapped without touching this handler or its routes.
	lh := &handlers.LibraryHandler{Library: lib}

	r := chi.NewRouter()

	// CORS middleware. The frontend (localhost:5173) calls this service (localhost:8080),
	// which is cross-origin. Authorization must be listed explicitly so browsers permit
	// the Bearer token header on admin routes. HLS.js segment requests never reach this
	// service — they go directly to Cloudflare R2 using SigV4 query params.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			// Browsers preflight cross-origin requests that carry custom headers with
			// OPTIONS. Respond 204 and return early — the real handler must not run.
			if req.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	// Auth routes. Both are public — /auth/token validates credentials and issues tokens,
	// /auth/refresh is a stub for the demo.
	r.Post("/auth/token", h.Token)
	r.Post("/auth/refresh", h.Refresh)

	// Admin routes are protected by JWT middleware scoped to this group only.
	// /auth/* and /health remain public — middleware is not applied globally.
	r.Route("/admin", func(r chi.Router) {
		r.Use(middleware.Authenticate(jwtSecret))
		r.Get("/tracks", lh.ListTracks)
		r.Post("/tracks", lh.AddTrack)
		r.Delete("/tracks/{id}", lh.DeactivateTrack)
	})

	// Liveness probe. No auth required — it reveals nothing sensitive.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// log.Fatal because ListenAndServe only returns on error; a silent return would
	// leave the process alive without serving. This is the only backend in the system —
	// Cloudflare R2 handles all segment delivery.
	log.Fatal(http.ListenAndServe(":8080", r))
}

// requireEnv reads a required environment variable and calls log.Fatal if absent.
// All credentials must be present before the service starts — missing values would
// cause runtime failures inside handlers, so failing here gives a clearer error.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}
