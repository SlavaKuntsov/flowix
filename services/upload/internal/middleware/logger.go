package middleware

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// RequestLogger — structured logging via zerolog, includes trace_id=request_id
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &respWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		dur := time.Since(start)
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = r.Header.Get("X-Request-Id")
		}
		if reqID == "" {
			reqID = r.Header.Get("X-Correlation-ID")
		}
		ev := log.Info()
		if ww.status >= 500 {
			ev = log.Error()
		} else if ww.status >= 400 {
			ev = log.Warn()
		}
		ev.Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.status).
			Dur("duration", dur).
			Str("trace_id", reqID).
			Str("request_id", reqID).
			Str("service", "upload").
			Msg("request")
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
