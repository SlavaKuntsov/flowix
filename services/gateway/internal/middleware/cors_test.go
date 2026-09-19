package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSPreflight(t *testing.T) {
	h := CORS([]string{"http://localhost:3000"}, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not call next for OPTIONS")
	}))
	req := httptest.NewRequest("OPTIONS", "/api/v1/videos", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("want 204 got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("want origin header got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatalf("want allow methods")
	}
}

func TestCORSSimple(t *testing.T) {
	h := CORS([]string{"http://localhost:3000"}, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/api/v1/videos", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	// issue #52: отражаем конкретный allowlisted origin, не wildcard
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("want origin got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("want Vary: Origin got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials should stay enabled for allowed origin, got %q", got)
	}
}

func TestCORSAllowedOriginsFilter(t *testing.T) {
	h := CORS([]string{"http://allowed.com"}, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("evil origin should not be allowed got %q", got)
	}
}

// issue #52: wildcard "*" больше не разрешает любой origin с credentials
func TestCORSWildcardRejected(t *testing.T) {
	h := CORS([]string{"*"}, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("wildcard must be ignored, got ACAO %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials must not be set for unknown origin, got %q", got)
	}
}

// issue #52: пустой allowlist — safe default, никому не отдаём ACAO
func TestCORSEmptyAllowlist(t *testing.T) {
	h := CORS(nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("empty allowlist must not produce ACAO, got %q", got)
	}
}
