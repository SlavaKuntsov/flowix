package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ParseTrustedCIDRS — парсит comma-separated список CIDR (env TRUSTED_PROXY_CIDRS,
// issue #52). X-Forwarded-For / X-Real-IP доверяем только если пирыый адрес
// запроса попадает в одну из этих сетей.
func ParseTrustedCIDRS(s string) ([]*net.IPNet, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var cidrs []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// голый IP (например "10.0.0.1") трактуем как /32 или /128
		if !strings.Contains(part, "/") {
			if ip := net.ParseIP(part); ip != nil && ip.To4() == nil {
				part += "/128"
			} else {
				part += "/32"
			}
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		cidrs = append(cidrs, n)
	}
	return cidrs, nil
}

// ClientIP — реальный IP клиента (issue #52). Клиентский X-Forwarded-For
// подделываем: доверяем ему только когда непосредственный пир (r.RemoteAddr)
// из trusted CIDR (наш прокси/LB). Из XFF берём самый правый недоверенный IP
// (стандартный обход спуфинга через цепочку прокси). Иначе — RemoteAddr.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := remoteHost(r)
	if !isTrustedIP(peer, trusted) {
		return peer
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := rightmostUntrusted(xff, trusted); ip != "" {
			return ip
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" && net.ParseIP(xri) != nil {
		return xri
	}
	return peer
}

// RealIP — замена chi middleware.RealIP с проверкой доверия (issue #52):
// RemoteAddr переписывается по XFF/X-Real-IP только от trusted прокси, а
// проверенный клиентский IP всегда выставляется в X-Real-IP для downstream
// (auth ключует лимитер по нему) — клиентские подделки затираются.
func RealIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, trusted)
			port := "0"
			if _, p, err := net.SplitHostPort(r.RemoteAddr); err == nil && p != "" {
				port = p
			}
			r.RemoteAddr = net.JoinHostPort(ip, port)
			r.Header.Set("X-Real-IP", ip)
			next.ServeHTTP(w, r)
		})
	}
}

// remoteHost — host-часть RemoteAddr (без порта).
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isTrustedIP(ipStr string, trusted []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// rightmostUntrusted — идём по XFF справа налево и возвращаем первый IP
// вне trusted CIDR (пропуская наши прокси, которые могли добавить свои хопы).
// Если все IP trusted — берём левый (ближайший к клиенту).
func rightmostUntrusted(xff string, trusted []*net.IPNet) string {
	parts := strings.Split(xff, ",")
	leftmost := ""
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		ip := net.ParseIP(p)
		if ip == nil {
			continue
		}
		if leftmost == "" {
			leftmost = ip.String()
		}
		if !isTrustedIP(p, trusted) {
			return ip.String()
		}
	}
	return leftmost
}
