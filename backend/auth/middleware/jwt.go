package middleware

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Authenticate returns a Chi-compatible middleware that validates JWT Bearer tokens.
// jwtSecret is closed over at construction time — the middleware never reads from
// the environment directly, keeping it testable and decoupled from global state.
func Authenticate(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Require an Authorization header. Absence means unauthenticated.
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Expect the format "Bearer <token>". Anything else is rejected —
			// query-parameter auth is Cloudflare's concern (SigV4), not this service's.
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			tokenString := parts[1]

			// Parse and validate the token. The keyFunc checks the signing method
			// before returning the secret — this blocks algorithm-confusion attacks
			// where a token forged with RS256 is accepted by an HMAC validator.
			// golang-jwt/jwt/v5 also validates exp automatically during Parse.
			token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			})
			if err != nil || !token.Valid {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Token is valid — pass the request to the next handler.
			next.ServeHTTP(w, r)
		})
	}
}
