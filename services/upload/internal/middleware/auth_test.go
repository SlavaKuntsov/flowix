// Package middleware tests — T3 upload auth.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func mustToken(secret, sub string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub})
	s, _ := t.SignedString([]byte(secret))
	return s
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
