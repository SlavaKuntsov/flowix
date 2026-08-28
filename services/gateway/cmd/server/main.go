package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"flowix/gateway/internal/handler"
	gwmw "flowix/gateway/internal/middleware"
	"flowix/gateway/internal/proxy"
)

func main() {
	port := envOr("GATEWAY_PORT", "8080")
	jwtSecret := envOr("JWT_SECRET", "change-me-super-secret-jwt-key-32chars")
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
	r.Use(gwmw.RequestLogger)

	// health — без прокси, без rate-limit (rate-limit уже пропускает /health)
	r.Get("/health", healthHandler)
	r.Get("/healthz", healthHandler)
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
	// Handle для POST — защищён, GET на этот путь не существует (пойдёт в metadata 404)
	r.With(authMw).Post("/api/v1/videos/upload", uploadProxy.ServeHTTP)
	r.With(authMw).Post("/api/v1/videos/upload/*", uploadProxy.ServeHTTP)

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

	// --- HLS / VOD: публичный, прокси на nginx-vod (JIT) ---
	// nginx-vod отдаёт master.m3u8 и сегменты; кэш заголовки ставит сам.
	r.Handle("/hls/*", vodProxy)

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
