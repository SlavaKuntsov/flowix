package middleware

import (
	"crypto/subtle"
	"net/http"
)

// InternalAuth protects /internal/* with shared secret header X-Internal-Token.
// If secret is empty (dev), it allows all — avoids breaking `make dev-*` without .env.
// Non-dev startup fails fast on an empty token (issue #53), so the bypass is
// dev-only. Comparison is constant-time (issue #53).
func InternalAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				next.ServeHTTP(w, r)
				return
			}
			got := []byte(r.Header.Get("X-Internal-Token"))
			if subtle.ConstantTimeCompare(got, []byte(secret)) != 1 {
				http.Error(w, `{"error":"unauthorized internal"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
