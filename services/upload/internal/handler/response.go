package handler

import (
	"log/slog"
	"net/http"

	"flowix/pkg/httputil"
)

// writeJSON — общий JSON-ответ через pkg/httputil (как в metadata, issue #63).
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	httputil.WriteJSON(w, r, status, v)
}

// writeError отдаёт клиенту безопасное сообщение через json.NewEncoder
// (issue #59: err.Error() не интерполируется в рукописный JSON — спецсимволы
// не ломают тело, детали инфры (endpoint'ы MinIO/metadata) не утекают).
// Детали ошибки — только в лог.
func writeError(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	if err != nil {
		slog.Error("upload handler error", "error", err, "path", r.URL.Path, "status", status)
	}
	writeJSON(w, r, status, map[string]string{"error": msg})
}
