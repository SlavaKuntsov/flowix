// Package middleware provides HTTP middleware for gateway authentication.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey string

const UserIDKey ctxKey = "user_id"

// AuthMiddleware проверяет JWT Bearer и кладёт sub (user_id) в контекст
// и заголовок X-User-ID для downstream сервисов. Пропускает без токена
// только если вызывающий явно не требует аутентификации.
func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(h, "Bearer ")
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			sub, _ := claims["sub"].(string)
			if sub == "" {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			// тип токена должен быть access (опционально, если поле есть)
			if typ, ok := claims["type"].(string); ok && typ != "" && typ != "access" {
				http.Error(w, `{"error":"invalid token type"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, sub)
			// проброс для metadata/upload — они тоже проверяют JWT, но X-User-ID удобен
			r2 := r.WithContext(ctx)
			r2.Header.Set("X-User-ID", sub)
			next.ServeHTTP(w, r2)
		})
	}
}

// OptionalAuth — как Auth, но не требует токен: если токен есть — валидирует
// и кладёт в контекст, если нет — пропускает. Удобно для публичных GET.
func OptionalAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			tokenStr := strings.TrimPrefix(h, "Bearer ")
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || !token.Valid {
				// невалидный токен даже в optional — 401, чтобы клиент знал
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			claims, _ := token.Claims.(jwt.MapClaims)
			sub, _ := claims["sub"].(string)
			if sub != "" {
				ctx := context.WithValue(r.Context(), UserIDKey, sub)
				r2 := r.WithContext(ctx)
				r2.Header.Set("X-User-ID", sub)
				next.ServeHTTP(w, r2)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func UserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}
