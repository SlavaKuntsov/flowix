package httpserver

import (
	"net/http"
	"testing"
	"time"
)

func TestDefaultsMatchIssue(t *testing.T) {
	d := Defaults()
	if d.ReadTimeout != 15*time.Second || d.WriteTimeout != 60*time.Second || d.IdleTimeout != 120*time.Second || d.ReadHeaderTimeout != 15*time.Second {
		t.Fatalf("issue #63 timeouts mismatch: %+v", d)
	}
	if d.ShutdownGrace <= 0 {
		t.Fatalf("shutdown grace must be positive: %+v", d)
	}
}

func TestStreamingHasNoBodyLimits(t *testing.T) {
	s := Streaming()
	if s.ReadTimeout != 0 || s.WriteTimeout != 0 {
		t.Fatalf("streaming config must not cap body read/write: %+v", s)
	}
	if s.ReadHeaderTimeout != 15*time.Second || s.IdleTimeout != 120*time.Second {
		t.Fatalf("streaming config mismatch: %+v", s)
	}
}

func TestNewServerAppliesTimeouts(t *testing.T) {
	tm := Defaults()
	srv := newServer(":0", http.NotFoundHandler(), tm)
	if srv.ReadTimeout != tm.ReadTimeout || srv.WriteTimeout != tm.WriteTimeout ||
		srv.IdleTimeout != tm.IdleTimeout || srv.ReadHeaderTimeout != tm.ReadHeaderTimeout {
		t.Fatalf("server timeouts not applied: %+v", srv)
	}
}
