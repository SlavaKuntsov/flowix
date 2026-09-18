package main

// @title Flowix Upload API
// @version 1.0
// @description Upload video to MinIO + publish video.uploaded event. JWT required.
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer {token}"
import (
	"log"
	"net/http"
	"os"
	"strconv"

	pkghttp "flowix/pkg/httpserver"
	pkglogger "flowix/pkg/logger"
	"flowix/pkg/metrics"
	pkgmw "flowix/pkg/middleware"
	_ "flowix/upload/docs"
	"flowix/upload/internal/client"
	"flowix/upload/internal/handler"
	"flowix/upload/internal/queue"
	"flowix/upload/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	port := os.Getenv("UPLOAD_PORT")
	if port == "" {
		port = "8003"
	}
	jwtSecret := requireEnv("JWT_SECRET")
	minioEndpoint := os.Getenv("MINIO_ENDPOINT")
	if minioEndpoint == "" {
		minioEndpoint = "minio:9000"
	}
	bucket := os.Getenv("VIDEO_STORAGE_BUCKET")
	if bucket == "" {
		bucket = "videos"
	}
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://flowix:flowix@rabbitmq:5672/"
	}
	metadataURL := os.Getenv("METADATA_URL")
	if metadataURL == "" {
		metadataURL = "http://metadata:8002"
	}
	minioAccess := os.Getenv("MINIO_ACCESS_KEY")
	minioSecret := os.Getenv("MINIO_SECRET_KEY")

	secure, _ := strconv.ParseBool(os.Getenv("MINIO_SECURE"))
	// zerolog console in dev, json in prod — общий bootstrap (issue #63)
	logger := pkglogger.Setup("upload")

	store, err := storage.NewMinioClient(minioEndpoint, minioAccess, minioSecret, bucket, secure)
	if err != nil {
		log.Fatalf("minio: %v", err)
	}
	pub, err := queue.NewPublisher(rabbitURL, "video.uploaded")
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer pub.Close()

	metaCl := client.NewMetadataClient(metadataURL, requireEnv("INTERNAL_TOKEN"))
	uh := handler.NewUploadHandler(store, pub, metaCl)
	ph := handler.NewPresignHandler(store, pub, metaCl, metaCl)
	rh := handler.NewResumableHandler(store, metaCl)

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, pkgmw.RequestLogger("upload"))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"upload"}`))
	})
	r.Get("/metrics", metrics.Handler().ServeHTTP)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/doc.json", http.StatusMovedPermanently)
	})

	r.Group(func(r chi.Router) {
		r.Use(pkgmw.AuthMiddleware(jwtSecret))
		r.Post("/api/v1/videos/upload", uh.Upload)
		r.Post("/api/v1/videos/presign", ph.Presign)
		r.Post("/api/v1/videos/{id}/complete", ph.Complete)
		r.Post("/api/v1/videos/complete", ph.Complete)
		r.Get("/api/v1/videos/{id}/resumable", rh.Status)
		r.Put("/api/v1/videos/{id}/resumable", rh.Upload)
	})

	logger.Info().Str("port", port).Str("bucket", bucket).Str("metadata", metadataURL).Msg("upload starting")
	// Streaming(): тела до 5 ГБ не влезают в Read/Write timeout — лимитируем
	// только заголовки; Run делает graceful shutdown, in-flight загрузки
	// не обрываются (issue #63).
	if err := pkghttp.Run(":"+port, r, pkghttp.Streaming()); err != nil {
		log.Fatal(err)
	}
}

// requireEnv reads a mandatory env var and exits with a clear error when it is
// empty — unless ENV=dev (local `make dev-*` runs without .env, issue #53).
func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" && os.Getenv("ENV") != "dev" {
		log.Fatalf("required environment variable %s is empty — set it in .env (see .env.example)", k)
	}
	return v
}
