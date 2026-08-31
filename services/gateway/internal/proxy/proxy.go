// Package proxy provides reverse-proxy helpers for gateway upstream services.
package proxy

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// New создает ReverseProxy на target. StripPrefix — опционально отрезает
// префикс пути перед проксированием (для /hls не нужен, для /api — не режем).
func New(target *url.URL) *httputil.ReverseProxy {
	p := httputil.NewSingleHostReverseProxy(target)
	origDirector := p.Director
	p.Director = func(r *http.Request) {
		origDirector(r)
		// SingleHostReverseProxy уже выставил Scheme/Host/Path.
		// Сохраняем оригинальный Host заголовок клиента в X-Forwarded-Host,
		// а Host выставляем на upstream (важно для nginx-vod Host header).
		r.Header.Set("X-Forwarded-Host", r.Host)
		// Прокидываем X-Request-ID (chi middleware.RequestID) во все апстримы для трассировки
		if v := r.Header.Get("X-Request-Id"); v != "" {
			r.Header.Set("X-Request-ID", v)
		}
		if v := r.Header.Get("X-Request-ID"); v != "" {
			// keep as is, ensure downstream sees it
			r.Header.Set("X-Request-ID", v)
		}
		// X-User-ID уже ставит AuthMiddleware — не трогаем, просто прокидываем если есть
		// X-Forwarded-For: добавляем client IP (gateway — единственная точка входа, поэтому формируем цепочку)
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if host != "" {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				// не дублируем если уже содержит host
				if !strings.Contains(xff, host) {
					r.Header.Set("X-Forwarded-For", xff+", "+host)
				}
			} else {
				r.Header.Set("X-Forwarded-For", host)
			}
		}
		// Заголовок X-Forwarded-Proto
		if r.TLS != nil {
			r.Header.Set("X-Forwarded-Proto", "https")
		} else if r.Header.Get("X-Forwarded-Proto") == "" {
			r.Header.Set("X-Forwarded-Proto", "http")
		}
	}
	p.ModifyResponse = func(resp *http.Response) error {
		// Strip upstream CORS headers — gateway is sole CORS authority.
		// Without this, upstream like auth (if it ever adds CORS) + gateway
		// produce duplicate `Access-Control-Allow-Origin: http://localhost:3000, *`
		// which browser rejects.
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Credentials")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Expose-Headers")
		resp.Header.Del("Access-Control-Max-Age")
		resp.Header.Del("Vary")
		return nil
	}
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error().Err(err).Str("target", target.String()).Str("path", r.URL.Path).Msg("proxy error")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
	}
	return p
}

// NewWithPrefix — как New, но отрезает prefix из пути перед проксированием.
// Например, если gateway слушает /api/v1/auth и хочет проксировать на auth:8001/api/v1/auth,
// то prefix="" (не режем). Если апстрим без префикса — указываем prefix.
func NewWithPrefix(target *url.URL, prefix string) *httputil.ReverseProxy {
	p := New(target)
	if prefix == "" {
		return p
	}
	origDirector := p.Director
	// оборачиваем ещё раз для strip
	p.Director = func(r *http.Request) {
		origDirector(r)
		// Director уже выставил URL на target+originalPath, теперь режем prefix
		// Нужно работать с r.URL.Path, который сейчас = target.Path + originalPath
		// Проще — режем из r.URL.Path, считая что target.Path == "" или "/"
		if strings.HasPrefix(r.URL.Path, prefix) {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}
	}
	return p
}
