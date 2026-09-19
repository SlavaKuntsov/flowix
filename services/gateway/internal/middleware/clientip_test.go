package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func mustCIDRS(t *testing.T, s string) []*net.IPNet {
	t.Helper()
	cidrs, err := ParseTrustedCIDRS(s)
	if err != nil {
		t.Fatalf("ParseTrustedCIDRS(%q): %v", s, err)
	}
	return cidrs
}

// issue #52: forged XFF от недоверенного пира не меняет client IP
func TestClientIPUntrustedPeerIgnoresXFF(t *testing.T) {
	cidrs := mustCIDRS(t, "172.16.0.0/12")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.5.5.5:1234" // пиер не из trusted сети
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.9")
	r.Header.Set("X-Real-IP", "6.6.6.6")
	if got := ClientIP(r, cidrs); got != "5.5.5.5" {
		t.Fatalf("want peer 5.5.5.5, got %q", got)
	}
	// без trusted CIDR — тоже RemoteAddr
	if got := ClientIP(r, nil); got != "5.5.5.5" {
		t.Fatalf("want peer 5.5.5.5 with nil trusted, got %q", got)
	}
}

// issue #52: trusted proxy → берём самый правый недоверенный IP из XFF
func TestClientIPTrustedProxy(t *testing.T) {
	cidrs := mustCIDRS(t, "172.16.0.0/12")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.5:443" // gateway за прокси в docker-сети
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 172.18.0.5")
	if got := ClientIP(r, cidrs); got != "1.2.3.4" {
		t.Fatalf("want 1.2.3.4, got %q", got)
	}
	// forged хвост (клиент сам подсунул 9.9.9.9, а прокси добавил реальный peer)
	// → правый недоверенный всё равно 1.2.3.4? Нет: XFF = "1.2.3.4, 9.9.9.9"
	// от trusted прокси означает что 9.9.9.9 видел прокси — доверяем правому.
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "172.18.0.5:443"
	r2.Header.Set("X-Forwarded-For", "1.2.3.4, 9.9.9.9")
	if got := ClientIP(r2, cidrs); got != "9.9.9.9" {
		t.Fatalf("rightmost untrusted wins, want 9.9.9.9 got %q", got)
	}
	// X-Real-IP от trusted прокси без XFF
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.RemoteAddr = "172.18.0.5:443"
	r3.Header.Set("X-Real-IP", "7.7.7.7")
	if got := ClientIP(r3, cidrs); got != "7.7.7.7" {
		t.Fatalf("want 7.7.7.7 got %q", got)
	}
}

// issue #52: RealIP middleware переписывает RemoteAddr + выставляет проверенный X-Real-IP
func TestRealIPTrustedMiddleware(t *testing.T) {
	cidrs := mustCIDRS(t, "172.16.0.0/12")
	h := RealIP(cidrs)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host := remoteHost(r); host != "1.2.3.4" {
			t.Errorf("want RemoteAddr host 1.2.3.4 got %q", host)
		}
		if got := r.Header.Get("X-Real-IP"); got != "1.2.3.4" {
			t.Errorf("want X-Real-IP 1.2.3.4 got %q", got)
		}
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	h.ServeHTTP(httptest.NewRecorder(), req)
}

// issue #52: недоверенный клиент не может подделать X-Real-IP — затирается
func TestRealIPUntrustedOverwritesForgedHeader(t *testing.T) {
	h := RealIP(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Real-IP"); got != "8.8.8.8" {
			t.Errorf("forged X-Real-IP must be overwritten with peer, got %q", got)
		}
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:9999"
	req.Header.Set("X-Real-IP", "1.1.1.1")
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	h.ServeHTTP(httptest.NewRecorder(), req)
}

// issue #52 (verify): подделка XFF не обходит лимит — два «разных» forged IP
// с одного недоверенного пира делят один бакет
func TestRateLimitForgedXFFSharesLimit(t *testing.T) {
	rdb, _ := newTestRedis(t)
	cidrs := mustCIDRS(t, "172.16.0.0/12")
	h := RateLimit(1, 2, rdb, cidrs, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	// один и тот же peer 5.5.5.5 (не trusted), «разные» forged XFF
	for i, xff := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		req := httptest.NewRequest("GET", "/api/v1/videos", nil)
		req.RemoteAddr = "5.5.5.5:1000"
		req.Header.Set("X-Forwarded-For", xff)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if i < 2 && w.Code != 200 {
			t.Fatalf("iter %d want 200 (burst 2), got %d", i, w.Code)
		}
		if i == 2 && w.Code != 429 {
			t.Fatalf("forged XFF must not bypass limit, want 429 got %d", w.Code)
		}
	}
}
