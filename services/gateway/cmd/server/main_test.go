package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testRouterConfig() routerConfig {
	return routerConfig{
		jwtSecret:      "test-secret-key-at-least-32-characters",
		internalToken:  "test-internal-token",
		uploadMaxBytes: 5 << 30,
		authURL:        "http://127.0.0.1:1",
		metadataURL:    "http://127.0.0.1:1",
		uploadURL:      "http://127.0.0.1:1",
		vodURL:         "http://127.0.0.1:1",
	}
}

// TestNoAnonymousThumbnailsProxy is the issue #45 regression test: the old
// anonymous /thumbnails/* MinIO proxy must stay removed — thumbnails are
// served via presigned URLs from metadata (issue #43), never through the
// gateway without auth.
func TestNoAnonymousThumbnailsProxy(t *testing.T) {
	r := newRouter(testRouterConfig())
	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, path := range []string{
		"/thumbnails/vid-1/thumb.jpg",
		"/thumbnails/",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		// 404 = no route; anything else (200/302/403-from-proxy) would mean the
		// anonymous MinIO proxy is back.
		if resp.StatusCode != http.StatusNotFound {
			_ = resp.Body.Close()
			t.Errorf("GET %s = %d, want 404 (anonymous thumbnails proxy must not exist)", path, resp.StatusCode)
			continue
		}
		_ = resp.Body.Close()
	}
}
