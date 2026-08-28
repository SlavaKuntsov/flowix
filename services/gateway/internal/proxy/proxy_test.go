package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestProxyForwardsPathAndHeaders(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotXUser string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotXUser = r.Header.Get("X-User-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	u, _ := url.Parse(backend.URL)
	p := New(u)

	req := httptest.NewRequest("GET", "/api/v1/videos/123", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-User-ID", "u-123")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d body %s", w.Code, w.Body.String())
	}
	if gotPath != "/api/v1/videos/123" {
		t.Fatalf("want /api/v1/videos/123 got %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth not forwarded got %q", gotAuth)
	}
	if gotXUser != "u-123" {
		t.Fatalf("x-user-id not forwarded got %q", gotXUser)
	}
}

func TestProxyErrorHandler(t *testing.T) {
	// указываем на несуществующий порт → dial error → 502
	u, _ := url.Parse("http://127.0.0.1:19099")
	p := New(u)
	req := httptest.NewRequest("GET", "/api/v1/videos", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 502 {
		t.Fatalf("want 502 got %d body %s", w.Code, w.Body.String())
	}
}
