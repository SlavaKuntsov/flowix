package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

func TestRateLimitAllowsThenBlocks(t *testing.T) {
	rdb, _ := newTestRedis(t)
	// 2 burst → окно 1s, первые 2 проходят, 3-й — 429
	h := RateLimit(2, 2, rdb, nil, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
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
	rdb, _ := newTestRedis(t)
	h := RateLimit(1, 1, rdb, nil, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// issue #52: разные IP лимитируются независимо (state в Redis, ключ по ClientIP)
func TestRateLimitPerIP(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := RateLimit(1, 1, rdb, nil, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req1 := httptest.NewRequest("GET", "/api/v1/videos", nil)
	req1.RemoteAddr = "1.1.1.1:1000"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("first ip want 200 got %d", w1.Code)
	}
	// другой IP — свой бакет, не 429
	req2 := httptest.NewRequest("GET", "/api/v1/videos", nil)
	req2.RemoteAddr = "2.2.2.2:1000"
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("second ip should have own bucket got %d", w2.Code)
	}
}

// issue #52: Redis недоступен → fail-open (availability over strictness)
func TestRateLimitFailOpenWhenRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = rdb.Close() // соединение рвём — все EVAL будут падать
	mr.Close()

	called := false
	h := RateLimit(1, 1, rdb, nil, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/v1/videos", nil)
		req.RemoteAddr = "3.3.3.3:1000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("redis down must fail-open, iter %d got %d", i, w.Code)
		}
	}
	if !called {
		t.Fatal("handler must be reached when redis is down")
	}
}
