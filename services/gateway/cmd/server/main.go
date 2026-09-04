package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"flowix/gateway/internal/handler"
	gwmw "flowix/gateway/internal/middleware"
	"flowix/gateway/internal/metrics"
	"flowix/gateway/internal/proxy"
)

func main() {
	port := envOr("GATEWAY_PORT", "8080")
	jwtSecret := envOr("JWT_SECRET", "change-me-super-secret-jwt-key-32chars")
	internalToken := envOr("INTERNAL_TOKEN", "")
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

	// zerolog console in dev, json in prod
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "console" || os.Getenv("ENV") == "dev" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	} else {
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		if l, err := zerolog.ParseLevel(lvl); err == nil {
			zerolog.SetGlobalLevel(l)
		}
	}
	logger := log.With().Str("service", "gateway").Logger()

	authTarget := mustParseURL(authURL)
	metadataTarget := mustParseURL(metadataURL)
	uploadTarget := mustParseURL(uploadURL)
	vodTarget := mustParseURL(vodURL)
	bucket := envOr("VIDEO_STORAGE_BUCKET", "videos")
	minioURL := envOr("MINIO_URL", "http://minio:9000/"+bucket)
	minioTarget := mustParseURL(minioURL)

	authProxy := proxy.New(authTarget)
	metadataProxy := proxy.New(metadataTarget)
	uploadProxy := proxy.New(uploadTarget)
	vodProxy := proxy.New(vodTarget)
	// thumbnails/renditions are stored in MinIO bucket `videos`; gateway exposes /thumbnails/* via MinIO
	minioProxy := proxy.New(minioTarget)

	r := chi.NewRouter()
	// базовые chi middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	// gateway middleware: CORS + RateLimit + RequestLogger (zerolog)
	// CORS allow all for MVP (frontend :3000)
	r.Use(gwmw.CORS([]string{"*"}, nil, nil))
	r.Use(gwmw.RateLimit(20, 40))
	r.Use(gwmw.RequestLogger)

	// health — без прокси, без rate-limit (rate-limit уже пропускает /health)
	r.Get("/health", healthHandler)
	r.Get("/healthz", healthHandler)
	r.Get("/metrics", metrics.Handler().ServeHTTP)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"service": "gateway", "status": "ok"})
	})

	// aggregated Swagger — единая точка на gateway, разделённая по сервисам (tags: auth/videos/upload)
	docsH := handler.NewDocsHandler(authURL, metadataURL, uploadURL)
	r.Get("/docs", docsH.HandleDocsUI)
	r.Get("/docs/", docsH.HandleDocsUI)
	r.Get("/openapi.json", docsH.HandleMerged)
	r.Get("/openapi/auth.json", docsH.HandleAuthSpec)
	r.Get("/openapi/metadata.json", docsH.HandleMetadataSpec)
	r.Get("/openapi/upload.json", docsH.HandleUploadSpec)

	authMw := gwmw.AuthMiddleware(jwtSecret)

	// --- Auth service: все /api/v1/auth/* публичные, без JWT ---
	// chi wildcard: /api/v1/auth/* захватывает /api/v1/auth/login etc.
	r.Handle("/api/v1/auth", authProxy)
	r.Handle("/api/v1/auth/*", authProxy)

	// --- Upload service: только POST /api/v1/videos/upload требует JWT ---
	// Handle для POST — защищён, с лимитом 5-6GB (MaxBytesReader), прокидывает X-Internal-Token если нужен
	maxBytesMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, uploadMaxBytes)
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
	r.With(authMw).Put("/api/v1/videos/{id}/resumable", uploadProxy.ServeHTTP)

	// --- HLS token for private videos (signed URL 1h) — must be before generic /videos/* proxy ---
	r.With(authMw).Get("/api/v1/videos/{id}/hls-token", gwmw.HLSTokenHandler(jwtSecret, internalToken, metadataURL))

	// --- Metadata service ---
	// Публичные GET (лист и деталь) — без JWT
	r.Get("/api/v1/videos", metadataProxy.ServeHTTP)
	r.Get("/api/v1/videos/*", metadataProxy.ServeHTTP)

	// Защищённые мутации metadata — требуют JWT
	r.With(authMw).Post("/api/v1/videos", metadataProxy.ServeHTTP)
	r.With(authMw).Post("/api/v1/videos/", metadataProxy.ServeHTTP)
	r.With(authMw).Patch("/api/v1/videos/*", metadataProxy.ServeHTTP)
	r.With(authMw).Delete("/api/v1/videos/*", metadataProxy.ServeHTTP)
	r.With(authMw).Put("/api/v1/videos/*", metadataProxy.ServeHTTP)

	// --- HLS / VOD: защищён HLSAuth (private 403 без токена, public пропуск) ---
	hlsAuth := gwmw.HLSAuth(jwtSecret, internalToken, metadataURL)
	r.With(hlsAuth, metrics.Middleware).Handle("/hls/*", vodProxy)

	// --- Thumbnails / public MinIO objects via gateway (avoid direct :9000 CORS) ---
	// frontend uses /thumbnails/{id}/thumb.jpg ; gateway proxies to MinIO bucket `videos`
	r.Handle("/thumbnails/*", minioProxy)
	r.Handle("/thumbnails", minioProxy)

	// Inject X-Internal-Token for internal downstream calls (metadata internal/*, nginx vod mapping)
	if internalToken != "" {
		origMetaDirector := metadataProxy.Director
		metadataProxy.Director = func(r *http.Request) {
			origMetaDirector(r)
			r.Header.Set("X-Internal-Token", internalToken)
		}
		origVodDirector := vodProxy.Director
		vodProxy.Director = func(r *http.Request) {
			origVodDirector(r)
			r.Header.Set("X-Internal-Token", internalToken)
		}
	}

	logger.Info().
		Str("port", port).
		Str("auth", authURL).
		Str("metadata", metadataURL).
		Str("upload", uploadURL).
		Str("vod", vodURL).
		Msg("gateway starting")

	if err := http.ListenAndServe(":"+port, r); err != nil {
		logger.Fatal().Err(err).Msg("gateway stopped")
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gateway"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
