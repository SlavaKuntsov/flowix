// Package middleware tests — T3 upload auth.
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

// issue #53: token without exp or type=access must be rejected.
func TestAuthRejectsTokenWithoutExpOrType(t *testing.T) {
	secret := "test-secret"
	noExp := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u1", "type": "access"})
	noType := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
	for name, tok := range map[string]*jwt.Token{"no_exp": noExp, "no_type": noType} {
		s, _ := tok.SignedString([]byte(secret))
		h := AuthMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") }))
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+s)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("%s: want 401 got %d", name, w.Code)
		}
	}
}

func TestAuthOK(t *testing.T) {
	secret := "test-secret-32chars-for-upload-tests!!"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserIDFromCtx(r.Context()) != "u1" {
			t.Fatalf("want u1 got %s", UserIDFromCtx(r.Context()))
		}
		w.WriteHeader(200)
	})
	h := AuthMiddleware(secret)(next)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+mustToken(secret, "u1"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

func TestAuthMissing(t *testing.T) {
	h := AuthMiddleware("s")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("no call") }))
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestAuthInvalid(t *testing.T) {
	h := AuthMiddleware("s")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("no call") }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer bad.token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}
