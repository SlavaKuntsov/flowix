package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetVideoOwner(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Internal-Token")
		w.Header().Set("Content-Type", "application/json")
		if gotToken != "test-internal-token" {
			http.Error(w, `{"error":"unauthorized internal"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/internal/videos/vid-1":
			_, _ = w.Write([]byte(`{"id":"vid-1","owner_id":"owner-1"}`))
		case "/internal/videos/vid-404":
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		default:
			http.Error(w, `{"error":"unauthorized internal"}`, http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	c := NewMetadataClient(srv.URL, "test-internal-token")

	owner, err := c.GetVideoOwner("vid-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if owner != "owner-1" {
		t.Fatalf("owner = %q, want owner-1", owner)
	}
	if gotPath != "/internal/videos/vid-1" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "test-internal-token" {
		t.Fatalf("X-Internal-Token = %q", gotToken)
	}

	// unknown video: error must carry "404" so handlers classify it as not-found
	_, err = c.GetVideoOwner("vid-404")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want it to contain 404", err)
	}

	// wrong token -> 401 from metadata, not classified as 404
	cNoToken := NewMetadataClient(srv.URL, "wrong-token")
	_, err = cNoToken.GetVideoOwner("vid-1")
	if err == nil || strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want non-404 for 401", err)
	}
}

func TestGetVideoOwnerOmitsEmptyToken(t *testing.T) {
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("X-Internal-Token") != ""
		_, _ = w.Write([]byte(`{"id":"v","owner_id":"o"}`))
	}))
	defer srv.Close()

	c := NewMetadataClient(srv.URL, "")
	if _, err := c.GetVideoOwner("v"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sawHeader {
		t.Fatal("X-Internal-Token must be omitted when internalToken is empty")
	}
}

func TestListStaleUploadedAndSetStatus(t *testing.T) {
	var gotQuery, gotToken, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Internal-Token")
		gotMethod = r.Method
		switch r.URL.Path {
		case "/internal/videos/stale":
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"data":[{"id":"v1","owner_id":"o1"}]}`))
		case "/internal/videos/v1/status":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewMetadataClient(srv.URL, "tok")
	list, err := c.ListStaleUploaded(2*time.Hour, 50)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(list) != 1 || list[0].ID != "v1" || list[0].OwnerID != "o1" {
		t.Fatalf("unexpected stale list %+v", list)
	}
	if gotQuery != "older_than_seconds=7200&limit=50" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotToken != "tok" {
		t.Fatalf("X-Internal-Token = %q", gotToken)
	}

	if err := c.SetStatus("v1", "failed"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotMethod != "PATCH" {
		t.Fatalf("method = %q, want PATCH", gotMethod)
	}
}
