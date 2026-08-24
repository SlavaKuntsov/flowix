package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
)

// writeJSON centralizes JSON encoding per golang-code-style + golang-error-handling.
//
// - Sets Content-Type explicitly (security: correct MIME, no sniffing)
// - Uses compact JSON by default (production: меньше трафика)
// - Pretty-print только по ?pretty=1 или PRETTY_JSON=true (dev: читаемость в Swagger/curl)
// - Логирует ошибку кодирования через slog (single handling rule: log server-side, не отдавать стек клиенту)
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	if shouldPretty(r) {
		enc.SetIndent("", "  ")
	}

	if err := enc.Encode(v); err != nil {
		// Encode after WriteHeader уже не поправить статус — только логируем
		slog.Error("encode json failed", "error", err, "path", r.URL.Path)
	}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	// security: не отдаем err.Error() с деталями БД/стека — только generic msg
	writeJSON(w, r, status, map[string]string{"error": msg})
}

func shouldPretty(r *http.Request) bool {
	// 1. явный ?pretty=1 / ?pretty=true
	if v := r.URL.Query().Get("pretty"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		// ?pretty без значения — считаем включенным
		return true
	}
	// 2. глобальный флаг для dev/Swagger: PRETTY_JSON=true
	if v := os.Getenv("PRETTY_JSON"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return false
}
