package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testVideoID = "00000000-0000-0000-0000-000000000001"

// shortenMetaCacheTTLs shrinks the cache TTLs for the test duration.
func shortenMetaCacheTTLs(t *testing.T) {
	t.Helper()
	oldTTL, oldNeg := metaCacheTTL, metaNegCacheTTL
	metaCacheTTL, metaNegCacheTTL = 20*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { metaCacheTTL, metaNegCacheTTL = oldTTL, oldNeg })
}

// newMetaStub starts a metadata stub returning the given status/visibility
// and counting internal video requests.
func newMetaStub(t *testing.T, status int, visibility func() string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":         testVideoID,
			"owner_id":   "owner-1",
			"visibility": visibility(),
			"status":     "ready",
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func hlsRequest(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func nextHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// (a) 200 authz checks for the same video → ~1-2 metadata HTTP requests.
func TestHLSAuthCachesMetadataLookups(t *testing.T) {
	srv, calls := newMetaStub(t, http.StatusOK, func() string { return "public" })
	h := HLSAuth("secret", "", srv.URL)(nextHandler())

	for i := 0; i < 200; i++ {
		w := hlsRequest(t, h, "/hls/"+testVideoID+"/seg-1.ts")
		if w.Code != http.StatusOK {
			t.Fatalf("req %d: want 200 got %d", i, w.Code)
		}
	}
	if got := atomic.LoadInt32(calls); got > 2 {
		t.Fatalf("want ≤2 metadata requests for 200 checks, got %d", got)
	}
	if got := atomic.LoadInt32(calls); got < 1 {
		t.Fatal("expected at least one metadata request")
	}
}

// (b) visibility change at metadata is observed within the cache TTL.
func TestHLSAuthVisibilityChangeObservedWithinTTL(t *testing.T) {
	shortenMetaCacheTTLs(t)
	var visibility atomic.Value
	visibility.Store("private")
	srv, calls := newMetaStub(t, http.StatusOK, func() string {
		return visibility.Load().(string)
	})
	h := HLSAuth("secret", "", srv.URL)(nextHandler())
	path := "/hls/" + testVideoID + "/master.m3u8"

	// private without token → 403
	if w := hlsRequest(t, h, path); w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for private video, got %d", w.Code)
	}
	// flip to public: still cached as private within TTL → 403
	visibility.Store("public")
	if w := hlsRequest(t, h, path); w.Code != http.StatusForbidden {
		t.Fatalf("want cached 403 within TTL, got %d", w.Code)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("want 1 metadata request before TTL expiry, got %d", got)
	}
	// after TTL the change is visible → 200 through to vod
	time.Sleep(40 * time.Millisecond)
	w := hlsRequest(t, h, path)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 after visibility change, got %d", w.Code)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("want 2 metadata requests after TTL expiry, got %d", got)
	}
}

// (c) a 404 from metadata is negatively cached for the short TTL.
func TestHLSAuthNegativeCache404(t *testing.T) {
	shortenMetaCacheTTLs(t)
	srv, calls := newMetaStub(t, http.StatusNotFound, nil)
	var next int32
	h := HLSAuth("secret", "", srv.URL)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&next, 1)
		w.WriteHeader(http.StatusNotFound) // vod would 404
	}))

	for i := 0; i < 10; i++ {
		if w := hlsRequest(t, h, "/hls/"+testVideoID+"/seg.ts"); w.Code != http.StatusNotFound {
			t.Fatalf("req %d: want 404 passthrough, got %d", i, w.Code)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("want 1 metadata request (negatively cached), got %d", got)
	}
	// after the negative TTL a fresh lookup happens
	time.Sleep(30 * time.Millisecond)
	if w := hlsRequest(t, h, "/hls/"+testVideoID+"/seg.ts"); w.Code != http.StatusNotFound {
		t.Fatalf("want 404 passthrough after TTL, got %d", w.Code)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("want 2 metadata requests after neg TTL, got %d", got)
	}
}

// (d) concurrent access is race-free (run with -race).
func TestHLSAuthConcurrentAccess(t *testing.T) {
	srv, calls := newMetaStub(t, http.StatusOK, func() string { return "public" })
	h := HLSAuth("secret", "", srv.URL)(nextHandler())

	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				w := hlsRequest(t, h, fmt.Sprintf("/hls/%s/seg-%d-%d.ts", testVideoID, g, i))
				if w.Code != http.StatusOK {
					t.Errorf("want 200 got %d", w.Code)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(calls); got < 1 {
		t.Fatal("expected at least one metadata request")
	}
}

// authz logic unchanged: private video allows only the owner (hls token or bearer).
func TestHLSAuthPrivateVideoAuthz(t *testing.T) {
	srv, _ := newMetaStub(t, http.StatusOK, func() string { return "private" })
	h := HLSAuth("secret", "", srv.URL)(nextHandler())
	path := "/hls/" + testVideoID + "/seg.ts"

	if w := hlsRequest(t, h, path); w.Code != http.StatusForbidden {
		t.Fatalf("want 403 without token, got %d", w.Code)
	}
	tok, err := GenerateHLSToken(testVideoID, "owner-1", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if w := hlsRequest(t, h, path+"?token="+tok); w.Code != http.StatusOK {
		t.Fatalf("want 200 with owner hls token, got %d", w.Code)
	}
}
