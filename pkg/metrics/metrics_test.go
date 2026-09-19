package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddlewareIncrementsVodCacheHit(t *testing.T) {
	before := testutil.ToFloat64(VodCacheHit)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/hls/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if after := testutil.ToFloat64(VodCacheHit); after != before+1 {
		t.Fatalf("vod_cache_hit want %v got %v", before+1, after)
	}
}

func TestHandlerNonNil(t *testing.T) {
	if Handler() == nil {
		t.Fatal("metrics handler must not be nil")
	}
}
