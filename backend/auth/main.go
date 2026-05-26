package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
)

func main() {
	// CF credentials and JWT_SECRET must be in the environment before any handler
	// initialises the AWS S3 client or signs tokens. godotenv.Load is a no-op when
	// .env is absent, so production deployments using real env vars are unaffected.
	godotenv.Load()

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
