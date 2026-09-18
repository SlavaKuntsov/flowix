package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// issue #53: InternalAuth must reject a wrong X-Internal-Token via
// constant-time comparison; a valid token passes, wrong/missing — 401.
func TestInternalAuth(t *testing.T) {
	h := InternalAuth("tok-123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"ok", "tok-123", 200},
		{"wrong", "tok-124", 401},
		{"empty", "", 401},
		{"prefix", "tok", 401},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("PATCH", "/internal/videos/00000000-0000-0000-0000-000000000000/status", nil)
		if tc.token != "" {
			req.Header.Set("X-Internal-Token", tc.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s: want %d got %d", tc.name, tc.want, w.Code)
		}
	}
}
