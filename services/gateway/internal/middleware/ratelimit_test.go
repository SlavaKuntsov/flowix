package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitAllowsThenBlocks(t *testing.T) {
	h := RateLimit(2, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	// 2 burst → первые 2 должны пройти, 3-й — 429
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/videos", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("iter %d want 200 got %d", i, w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/api/v1/videos", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Fatalf("want 429 got %d", w.Code)
	}
}

func TestRateLimitBypassesHealth(t *testing.T) {
	h := RateLimit(1, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	// исчерпываем лимит
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/v1/videos", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
	// health всё равно 200
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "9.9.9.9:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("health should bypass rate limit got %d", w.Code)
	}
}
