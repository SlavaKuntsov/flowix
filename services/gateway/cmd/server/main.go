package main

import (
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"

	"flowix/gateway/internal/handler"
	gwmw "flowix/gateway/internal/middleware"
	"flowix/gateway/internal/proxy"
	pkghttp "flowix/pkg/httpserver"
	"flowix/pkg/httputil"
	pkglogger "flowix/pkg/logger"
	"flowix/pkg/metrics"
	pkgmw "flowix/pkg/middleware"
)

type routerConfig struct {
	jwtSecret      string
	internalToken  string
	uploadMaxBytes int64
	authURL        string
	metadataURL    string
	uploadURL      string
	vodURL         string
}

func main() {
	port := envOr("GATEWAY_PORT", "8080")
	jwtSecret := requireEnv("JWT_SECRET")
	internalToken := requireEnv("INTERNAL_TOKEN")
	uploadMaxBytes := int64(5 << 30) // 5GB default for Phase 9
	if v := envOr("UPLOAD_MAX_BYTES", ""); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			uploadMaxBytes = n
		}
	}
	authURL := envOr("AUTH_URL", "http://auth:8001")
	metadataURL := envOr("METADATA_URL", "http://metadata:8002")
	uploadURL := envOr("UPLOAD_URL", "http://upload:8003")
	vodURL := envOr("VOD_URL", "http://nginx-vod:80")

	// also support legacy AUTH_URL without port fallback
	if authURL == "http://auth:8000" {
		authURL = "http://auth:8001"
	}

	// zerolog console in dev, json in prod — общий bootstrap (issue #63)
	logger := pkglogger.Setup("gateway")

	r := newRouter(routerConfig{
		jwtSecret:      jwtSecret,
		internalToken:  internalToken,
		uploadMaxBytes: uploadMaxBytes,
		authURL:        authURL,
		metadataURL:    metadataURL,
		uploadURL:      uploadURL,
		vodURL:         vodURL,
	})

	logger.Info().
		Str("port", port).
		Str("auth", authURL).
		Str("metadata", metadataURL).
		Str("upload", uploadURL).
		Str("vod", vodURL).
		Msg("gateway starting")

	// Streaming(): через gateway стримятся тела до 5 ГБ (upload-прокси) —
	// лимитируем только заголовки, иначе Read/Write timeout обрывает загрузки.
	// Run также делает graceful shutdown по SIGTERM/SIGINT (issue #63).
	if err := pkghttp.Run(":"+port, r, pkghttp.Streaming()); err != nil {
		logger.Fatal().Err(err).Msg("gateway stopped")
	}
}

func newRouter(cfg routerConfig) *chi.Mux {
	authTarget := mustParseURL(cfg.authURL)
	metadataTarget := mustParseURL(cfg.metadataURL)
	uploadTarget := mustParseURL(cfg.uploadURL)
	vodTarget := mustParseURL(cfg.vodURL)

	authProxy := proxy.New(authTarget)
	metadataProxy := proxy.New(metadataTarget)
	uploadProxy := proxy.New(uploadTarget)
	vodProxy := proxy.New(vodTarget)

	r := chi.NewRouter()
	// базовые chi middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	// gateway middleware: CORS + RateLimit + RequestLogger (zerolog)
	// CORS allow all for MVP (frontend :3000)
	r.Use(gwmw.CORS([]string{"*"}, nil, nil))
	r.Use(gwmw.RateLimit(20, 40))
	r.Use(pkgmw.RequestLogger("gateway"))

	// health — без прокси, без rate-limit (rate-limit уже пропускает /health)
	r.Get("/health", healthHandler)
	r.Get("/healthz", healthHandler)
	r.Get("/metrics", metrics.Handler().ServeHTTP)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteJSON(w, r, http.StatusOK, map[string]string{"service": "gateway", "status": "ok"})
	})

	// aggregated Swagger — единая точка на gateway, разделённая по сервисам (tags: auth/videos/upload)
	docsH := handler.NewDocsHandler(cfg.authURL, cfg.metadataURL, cfg.uploadURL)
	r.Get("/docs", docsH.HandleDocsUI)
	r.Get("/docs/", docsH.HandleDocsUI)
	r.Get("/openapi.json", docsH.HandleMerged)
	r.Get("/openapi/auth.json", docsH.HandleAuthSpec)
	r.Get("/openapi/metadata.json", docsH.HandleMetadataSpec)
	r.Get("/openapi/upload.json", docsH.HandleUploadSpec)

	authMw := pkgmw.AuthMiddleware(cfg.jwtSecret)

	// --- Auth service: все /api/v1/auth/* публичные, без JWT ---
	// chi wildcard: /api/v1/auth/* захватывает /api/v1/auth/login etc.
	r.Handle("/api/v1/auth", authProxy)
	r.Handle("/api/v1/auth/*", authProxy)

	// --- Upload service: только POST /api/v1/videos/upload требует JWT ---
	// Handle для POST — защищён, с лимитом 5-6GB (MaxBytesReader), прокидывает X-Internal-Token если нужен
	maxBytesMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.uploadMaxBytes)
			next.ServeHTTP(w, r)
		})
	}
	r.With(authMw, maxBytesMw).Post("/api/v1/videos/upload", uploadProxy.ServeHTTP)
	r.With(authMw, maxBytesMw).Post("/api/v1/videos/upload/*", uploadProxy.ServeHTTP)
	// --- Presigned upload: без MaxBytesReader (прямой PUT в MinIO), только JWT ---
	r.With(authMw).Post("/api/v1/videos/presign", uploadProxy.ServeHTTP)
	r.With(authMw).Post("/api/v1/videos/complete", uploadProxy.ServeHTTP)
	r.With(authMw).Post("/api/v1/videos/{id}/complete", uploadProxy.ServeHTTP)
	// --- Resumable Content-Range fallback (backlog) ---
	r.With(authMw).Get("/api/v1/videos/{id}/resumable", uploadProxy.ServeHTTP)
	r.With(authMw, maxBytesMw).Put("/api/v1/videos/{id}/resumable", uploadProxy.ServeHTTP)

	// --- HLS token for private videos (signed URL 1h) — must be before generic /videos/* proxy ---
	r.With(authMw).Get("/api/v1/videos/{id}/hls-token", gwmw.HLSTokenHandler(cfg.jwtSecret, cfg.internalToken, cfg.metadataURL))

	// --- Metadata service ---
	// Публичные GET (лист и деталь) — без обязательного JWT, но с OptionalAuth:
	// валидный Bearer превращается в X-User-ID, чтобы metadata отдала приватные
	// видео владельцу (issue #44)
	optAuth := pkgmw.OptionalAuth(cfg.jwtSecret)
	r.With(optAuth).Get("/api/v1/videos", metadataProxy.ServeHTTP)
	r.With(optAuth).Get("/api/v1/videos/*", metadataProxy.ServeHTTP)

	// Защищённые мутации metadata — требуют JWT
	r.With(authMw).Post("/api/v1/videos", metadataProxy.ServeHTTP)
	r.With(authMw).Post("/api/v1/videos/", metadataProxy.ServeHTTP)
	r.With(authMw).Patch("/api/v1/videos/*", metadataProxy.ServeHTTP)
	r.With(authMw).Delete("/api/v1/videos/*", metadataProxy.ServeHTTP)
	r.With(authMw).Put("/api/v1/videos/*", metadataProxy.ServeHTTP)

	// --- HLS / VOD: защищён HLSAuth (private 403 без токена, public пропуск) ---
	hlsAuth := gwmw.HLSAuth(cfg.jwtSecret, cfg.internalToken, cfg.metadataURL)
	r.With(hlsAuth, metrics.Middleware).Handle("/hls/*", vodProxy)

	// Thumbnails: since issue #43 the MinIO bucket is fully private — metadata returns
	// presigned absolute thumbnail_url (MINIO_PUBLIC_ENDPOINT); the old anonymous
	// /thumbnails/* MinIO proxy is removed (issue #45 regression test).

	// Inject X-Internal-Token for internal downstream calls (metadata internal/*, nginx vod mapping)
	if cfg.internalToken != "" {
		origMetaDirector := metadataProxy.Director
		metadataProxy.Director = func(r *http.Request) {
			origMetaDirector(r)
			r.Header.Set("X-Internal-Token", cfg.internalToken)
		}
		origVodDirector := vodProxy.Director
		vodProxy.Director = func(r *http.Request) {
			origVodDirector(r)
			r.Header.Set("X-Internal-Token", cfg.internalToken)
		}
	}

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	httputil.WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok", "service": "gateway"})
}

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		log.Fatal().Err(err).Str("url", s).Msg("invalid upstream url")
	}
	return u
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// requireEnv reads a mandatory env var and exits with a clear error when it is
// empty — unless ENV=dev (local `make dev-*` runs without .env, issue #53).
func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" && os.Getenv("ENV") != "dev" {
		log.Fatal().Str("env", k).Msg("required environment variable is empty — set it in .env (see .env.example)")
	}
	return v
}
