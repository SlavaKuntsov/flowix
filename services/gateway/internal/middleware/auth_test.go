package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func mustToken(secret, sub string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub, "type": "access"})
	s, _ := t.SignedString([]byte(secret))
	return s
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
	var sawHeader string
	h := OptionalAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("X-User-ID")
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "victim")
	req.Header.Set("Authorization", "Bearer "+mustToken(secret, "u1"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || sawHeader != "u1" {
		t.Fatalf("valid JWT must set X-User-ID=u1, got %q (code %d)", sawHeader, w.Code)
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
