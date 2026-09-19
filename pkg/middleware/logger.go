package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// RequestLogger — structured logging через zerolog. Логирует метод, путь,
// статус, длительность, IP и trace_id; service тегирует сервис-источник.
// Работает с chi RequestID (из контекста), иначе берёт id из заголовков
// клиента; найденный id прокидывается в заголовки запроса (для proxy в
// апстримы) и ответа (клиент может скопировать id для поиска в Grafana).
// Ошибки >=500 — уровнем Error.
func RequestLogger(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := chimw.GetReqID(r.Context())
			if reqID == "" {
				reqID = r.Header.Get("X-Request-ID")
			}
			if reqID == "" {
				reqID = r.Header.Get("X-Request-Id")
			}
			if reqID == "" {
				reqID = r.Header.Get("X-Correlation-ID")
			}
			if reqID != "" {
				r.Header.Set("X-Request-ID", reqID)
				w.Header().Set("X-Request-Id", reqID)
			}

			ww := &respWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)
			dur := time.Since(start)

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
				Str("ip", ClientIP(r)).
				Str("trace_id", reqID).
				Str("service", service).
				Msg("request")
			// также zerolog global logger доступен как zerolog.Ctx
			_ = zerolog.Ctx(r.Context())
		})
	}
}

// respWriter захватывает статус ответа и пробрасывает Flush в нижележащий
// writer (http.Flusher) — иначе стриминг сквозь обёртку (HLS-прокси,
// streaming-ответы) буферизуется, а ReverseProxy не может сбрасывать буфер.
type respWriter struct {
	http.ResponseWriter
	status int
}

func (w *respWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *respWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ClientIP извлекает IP клиента для логов. Клиентский X-Forwarded-For не
// доверяем (первый IP подделывается, issue #52): берём X-Real-IP — его
// выставляет gateway-RealIP из проверенного peer'а и затирает клиентские
// подделки, — иначе RemoteAddr (за gateway это peer самого gateway).
func ClientIP(r *http.Request) string {
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" && net.ParseIP(xri) != nil {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
