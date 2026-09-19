// Package handler implements HTTP handlers of the metadata API (videos CRUD, VOD mapping).
package handler

import (
	"net/http"

	"flowix/pkg/httputil"
)

// writeJSON — общий хелпер JSON-ответов (issue #63: единая реализация в pkg/httputil).
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	httputil.WriteJSON(w, r, status, v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	// security: не отдаем err.Error() с деталями БД/стека — только generic msg
	writeJSON(w, r, status, map[string]string{"error": msg})
}
