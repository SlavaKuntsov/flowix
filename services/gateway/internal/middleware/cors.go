package middleware

import "net/http"

// CORS возвращает middleware, который добавляет CORS-заголовки и обрабатывает preflight.
//
// Логика: проверяем origin — если задан список allowedOrigins, то только они
// (или "*" для любого). Иначе отражаем origin запроса — простой подход для MVP.
func CORS(allowedOrigins []string, allowedMethods []string, allowedHeaders []string) func(http.Handler) http.Handler {
	if len(allowedMethods) == 0 {
		allowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"}
	}
	if len(allowedHeaders) == 0 {
		allowedHeaders = []string{"Authorization", "Content-Type", "Accept", "Origin", "X-Requested-With", "Range"}
	}
	allowAll := len(allowedOrigins) == 0
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}
	// precompute header values
	methodsVal := join(allowedMethods, ", ")
	headersVal := join(allowedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// non-browser or same-origin — пропускаем без CORS заголовков
				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Methods", methodsVal)
					w.Header().Set("Access-Control-Allow-Headers", headersVal)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			allowed := allowAll
			if !allowAll {
				for _, o := range allowedOrigins {
					if o == origin {
						allowed = true
						break
					}
				}
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// для кэша вариантности
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", methodsVal)
			w.Header().Set("Access-Control-Allow-Headers", headersVal)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")
			// expose useful headers for hls / upload
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Content-Type")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func join(elems []string, sep string) string {
	if len(elems) == 0 {
		return ""
	}
	n := 0
	for _, e := range elems {
		n += len(e) + len(sep)
	}
	b := make([]byte, 0, n)
	for i, e := range elems {
		if i > 0 {
			b = append(b, sep...)
		}
		b = append(b, e...)
	}
	return string(b)
}
