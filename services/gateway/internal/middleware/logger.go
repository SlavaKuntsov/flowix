package middleware

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// RequestLogger — structured logging через zerolog. Логирует метод, путь,
// статус, длительность и IP. Ошибки >=500 — уровнем Error.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &respWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		dur := time.Since(start)

		ev := log.Info()
		if ww.status >= 500 {
			ev = log.Error()
		} else if ww.status >= 400 {
			ev = log.Warn()
		}
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = r.Header.Get("X-Request-Id")
		}
		ev.Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.status).
			Dur("duration", dur).
			Str("ip", clientIP(r)).
			Str("req_id", reqID).
			Str("trace_id", reqID).
			Str("request_id", reqID).
			Str("service", "gateway").
			Msg("request")
		// также zerolog global logger доступен как zerolog.Ctx
		_ = zerolog.Ctx(r.Context())
	})
}

type respWriter struct {
	http.ResponseWriter
	status int
}

func (w *respWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
