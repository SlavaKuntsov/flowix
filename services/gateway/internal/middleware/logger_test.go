package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func captureLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	orig := log.Logger
	buf := &bytes.Buffer{}
	log.Logger = zerolog.New(buf)
	return buf, func() { log.Logger = orig }
}

func TestRequestLoggerPropagatesGeneratedRequestID(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	var gotHeader string
	h := chimw.RequestID(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/videos", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if gotHeader == "" {
		t.Fatal("generated request id not set in r.Header for proxy forwarding")
	}
	if w.Header().Get("X-Request-Id") != gotHeader {
		t.Fatalf("response X-Request-Id %q != generated %q", w.Header().Get("X-Request-Id"), gotHeader)
	}
	if !strings.Contains(buf.String(), `"trace_id":"`+gotHeader+`"`) {
		t.Fatalf("trace_id %q missing in log: %s", gotHeader, buf.String())
	}
}

func TestRequestLoggerKeepsClientRequestID(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	var gotHeader string
	h := chimw.RequestID(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/videos", nil)
	req.Header.Set("X-Request-ID", "client-trace-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if gotHeader != "client-trace-1" {
		t.Fatalf("client X-Request-ID overwritten: got %q", gotHeader)
	}
	if !strings.Contains(buf.String(), `"trace_id":"client-trace-1"`) {
		t.Fatalf("client trace_id missing in log: %s", buf.String())
	}
}
