package middleware

import "net/http"

// InternalAuth protects /internal/* with shared secret header X-Internal-Token.
// If secret is empty (dev), it allows all — avoids breaking `make dev-*` without .env.
// In prod secret must be set (see .env.example: INTERNAL_TOKEN).
func InternalAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("X-Internal-Token") != secret {
				http.Error(w, `{"error":"unauthorized internal"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
