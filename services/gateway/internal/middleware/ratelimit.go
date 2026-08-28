package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimit — per-IP токен-бакет. Параметры:
//
//	rps   — запросов в секунду (rate)
//	burst — размер бакета
//
// Для gateway дефолт 20 rps burst 40 достаточно для MVP и hls сегментов
// (браузер грузит ~1 сегмент / 2 сек + метаданные).
func RateLimit(rps int, burst int) func(http.Handler) http.Handler {
	if rps <= 0 {
		rps = 20
	}
	if burst <= 0 {
		burst = 40
	}
	type entry struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}
	var (
		mu       sync.Mutex
		visitors = make(map[string]*entry)
	)
	// фоновая очистка старых лимитеров (раз в минуту — не более ~10k IP)
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			mu.Lock()
			for ip, e := range visitors {
				if time.Since(e.lastSeen) > 3*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	getLimiter := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		if e, ok := visitors[ip]; ok {
			e.lastSeen = time.Now()
			return e.limiter
		}
		lim := rate.NewLimiter(rate.Limit(rps), burst)
		visitors[ip] = &entry{limiter: lim, lastSeen: time.Now()}
		return lim
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// health не лимитируем — иначе k8s probe может получить 429
			if r.URL.Path == "/health" || r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			ip := clientIP(r)
			lim := getLimiter(ip)
			if !lim.Allow() {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	// учитываем X-Forwarded-For (когда gateway за CDN / LB)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// берём первый IP
		for i, c := range xff {
			if c == ',' {
				return trimSpace(xff[:i])
			}
		}
		return trimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trimSpace(s string) string {
	// inline strings.TrimSpace без импорта strings для одного места,
	// но проще импорт — оставим ручную версию для отсутствия extra import
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
