package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func mustToken(secret, sub string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  sub,
		"type": "access",
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	s, _ := t.SignedString([]byte(secret))
	return s
}

// issue #53: token without exp must be rejected by AuthMiddleware.
func TestAuthRejectsTokenWithoutExp(t *testing.T) {
	secret := "test-secret"
	tok := func() string {
		tt := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u1", "type": "access"})
		s, _ := tt.SignedString([]byte(secret))
		return s
	}()
	h := AuthMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 for token without exp got %d", w.Code)
	}
}

// issue #53: token without type claim must be rejected (type=access required).
func TestAuthRejectsTokenWithoutType(t *testing.T) {
	secret := "test-secret"
	tok := func() string {
		tt := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "u1",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		s, _ := tt.SignedString([]byte(secret))
		return s
	}()
	h := AuthMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 for token without type got %d", w.Code)
	}
}

func TestAuthMiddlewareOK(t *testing.T) {
	secret := "test-secret-32chars-for-unit-tests-!!"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if UserIDFromCtx(r.Context()) != "u1" {
			t.Fatalf("want u1 got %s", UserIDFromCtx(r.Context()))
		}
		if r.Header.Get("X-User-ID") != "u1" {
			t.Fatalf("want header u1 got %s", r.Header.Get("X-User-ID"))
		}
		w.WriteHeader(200)
	})
	h := AuthMiddleware(secret)(next)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+mustToken(secret, "u1"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !called {
		t.Fatalf("want 200 called true got %d %v", w.Code, called)
	}
}

func TestAuthMiddlewareMissing(t *testing.T) {
	h := AuthMiddleware("s")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") }))
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestAuthMiddlewareInvalid(t *testing.T) {
	h := AuthMiddleware("s")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestAuthRejectsRefreshToken(t *testing.T) {
	secret := "test-secret"
	tok := func() string {
		tt := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u1", "type": "refresh"})
		s, _ := tt.SignedString([]byte(secret))
		return s
	}()
	h := AuthMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 for refresh token got %d", w.Code)
	}
}

// Issue #44: OptionalAuth must strip a client-supplied X-User-ID — it is the
// trust boundary for owner-scoped visibility on public video reads.
func TestOptionalAuthStripsForgedUserID(t *testing.T) {
	secret := "test-secret"
	var sawHeader string
	h := OptionalAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("X-User-ID")
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "victim")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || sawHeader != "" {
		t.Fatalf("forged X-User-ID must be stripped, got %q (code %d)", sawHeader, w.Code)
	}
}

func TestOptionalAuthSetsUserIDFromJWT(t *testing.T) {
	secret := "test-secret"
	var sawHeader, sawCtx string
	h := OptionalAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("X-User-ID")
		sawCtx = UserIDFromCtx(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "victim")
	req.Header.Set("Authorization", "Bearer "+mustToken(secret, "u1"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || sawHeader != "u1" || sawCtx != "u1" {
		t.Fatalf("valid JWT must set X-User-ID=u1, got header %q ctx %q (code %d)", sawHeader, sawCtx, w.Code)
	}
}

// Issue #44 (review): an expired/invalid token on a public endpoint must
// degrade to anonymous, not 401 — the frontend sends stored tokens on reads.
func TestOptionalAuthInvalidTokenIsAnonymous(t *testing.T) {
	secret := "test-secret"
	called := false
	var sawHeader string
	h := OptionalAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sawHeader = r.Header.Get("X-User-ID")
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "victim")
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !called || sawHeader != "" {
		t.Fatalf("invalid token must be anonymous, got code %d called %v header %q", w.Code, called, sawHeader)
	}
}

// Issue #44 review: a refresh token is not an access identity — OptionalAuth
// must treat it as anonymous (same rule as AuthMiddleware's type check).
func TestOptionalAuthRejectsRefreshToken(t *testing.T) {
	secret := "test-secret"
	var sawHeader string
	called := false
	h := OptionalAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sawHeader = r.Header.Get("X-User-ID")
		w.WriteHeader(200)
	}))
	refresh := func() string {
		tt := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u1", "type": "refresh"})
		s, _ := tt.SignedString([]byte(secret))
		return s
	}()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+refresh)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !called || sawHeader != "" {
		t.Fatalf("refresh token must be anonymous, got code %d called %v header %q", w.Code, called, sawHeader)
	}
}

func TestClientIPForwardedFor(t *testing.T) {
	// issue #52: клиентский XFF подделываем — не используем его для логов
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", " 203.0.113.7 , 10.0.0.1")
	r.RemoteAddr = "192.0.2.10:5555"
	if got := ClientIP(r); got != "192.0.2.10" {
		t.Fatalf("XFF must be ignored, want RemoteAddr ip, got %q", got)
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("X-Real-IP", "198.51.100.2")
	r2.RemoteAddr = "192.0.2.10:5555"
	if got := ClientIP(r2); got != "198.51.100.2" {
		t.Fatalf("want X-Real-IP, got %q", got)
	}
	// не-IP в X-Real-IP — не доверяем, падаем на RemoteAddr
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.Header.Set("X-Real-IP", "not-an-ip")
	r3.RemoteAddr = "192.0.2.10:5555"
	if got := ClientIP(r3); got != "192.0.2.10" {
		t.Fatalf("invalid X-Real-IP must be ignored, got %q", got)
	}
}
