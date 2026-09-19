package middleware

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// rateLimitScript — атомарный fixed-window (issue #52): INCR + PEXPIRE в одном
// EVAL, без гонок между инстансами gateway. KEYS[1] = bucket key,
// ARGV[1] = window ms, ARGV[2] = limit. Возвращает 1 (пропустить) / 0 (лимит).
var rateLimitScript = redis.NewScript(`
local cnt = redis.call('INCR', KEYS[1])
if cnt == 1 then
  redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[1]))
end
if cnt > tonumber(ARGV[2]) then
  return 0
end
return 1
`)

// RateLimit — per-IP fixed-window лимитер на Redis (issue #52): состояние
// общее для всех инстансов gateway, ключ — реальный IP клиента (XFF доверяем
// только от trusted прокси, см. ClientIP).
//
// Параметры:
//
//	rps     — запросов в секунду (sustained)
//	burst   — максимум за окно (окно = max(1s, burst/rps), т.е. ~40 запросов
//	          за 2с при 20rps/40burst — как прежний токен-бакет)
//	rdb     — Redis-клиент; nil или недоступный Redis → fail-open
//	          (availability over strictness), с warn в лог не чаще раза в минуту
func RateLimit(rps int, burst int, rdb *redis.Client, trusted []*net.IPNet, logger zerolog.Logger) func(http.Handler) http.Handler {
	if rps <= 0 {
		rps = 20
	}
	if burst <= 0 {
		burst = 40
	}
	window := time.Duration(burst/rps) * time.Second
	if window < time.Second {
		window = time.Second
	}
	windowMs := strconv.Itoa(int(window.Milliseconds()))
	limit := strconv.Itoa(burst)

	var (
		mu       sync.Mutex
		lastWarn time.Time
	)
	warnOpen := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(lastWarn) > time.Minute {
			lastWarn = time.Now()
			logger.Warn().Err(err).Msg("redis rate limiter unavailable — failing open")
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// health не лимитируем — иначе k8s probe может получить 429
			if r.URL.Path == "/health" || r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			if rdb != nil {
				ip := ClientIP(r, trusted)
				bucket := time.Now().UnixMilli() / window.Milliseconds()
				key := "ratelimit:" + ip + ":" + strconv.FormatInt(bucket, 10)
				allowed, err := rateLimitScript.Run(context.Background(), rdb, []string{key}, windowMs, limit).Int()
				if err != nil {
					warnOpen(err)
				} else if allowed == 0 {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.Header().Set("Retry-After", "1")
					http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
