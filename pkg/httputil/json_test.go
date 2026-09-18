package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONSetsContentTypeAndBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})

	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, w.Body.String())
	}
	if out["status"] != "ok" {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestWriteJSONPrettyByQuery(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/?pretty=true", nil)
	WriteJSON(w, r, http.StatusOK, map[string]string{"a": "b"})
	if !strings.Contains(w.Body.String(), "\n") || !strings.Contains(w.Body.String(), "  \"a\"") {
		t.Fatalf("expected indented json, got %q", w.Body.String())
	}
}

func TestWriteJSONPrettyByEnv(t *testing.T) {
	t.Setenv("PRETTY_JSON", "true")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	WriteJSON(w, r, http.StatusOK, map[string]string{"a": "b"})
	if !strings.Contains(w.Body.String(), "\n") {
		t.Fatalf("expected indented json via PRETTY_JSON, got %q", w.Body.String())
	}
}

func TestWriteJSONEscapeHTMLDisabled(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	WriteJSON(w, r, http.StatusOK, map[string]string{"u": "http://x/?a=1&b=2"})
	if strings.Contains(w.Body.String(), "\\u0026") {
		t.Fatalf("HTML escaping must be disabled, got %s", w.Body.String())
	}
}
