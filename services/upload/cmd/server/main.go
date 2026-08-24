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

	_ "flowix/upload/docs"
	"flowix/upload/internal/client"
	"flowix/upload/internal/handler"
	mw "flowix/upload/internal/middleware"
	"flowix/upload/internal/queue"
	"flowix/upload/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	port := os.Getenv("UPLOAD_PORT")
	if port == "" {
		port = "8003"
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "change-me-super-secret-jwt-key-32chars"
	}
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

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	store, err := storage.NewMinioClient(minioEndpoint, minioAccess, minioSecret, bucket, secure)
	if err != nil {
		log.Fatalf("minio: %v", err)
	}
	pub, err := queue.NewPublisher(rabbitURL, "video.uploaded")
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer pub.Close()

	metaCl := client.NewMetadataClient(metadataURL)
	uh := handler.NewUploadHandler(store, pub, metaCl)

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer, middleware.RequestID)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"upload"}`))
	})
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/doc.json", http.StatusMovedPermanently)
	})

	r.Group(func(r chi.Router) {
		r.Use(mw.AuthMiddleware(jwtSecret))
		r.Post("/api/v1/videos/upload", uh.Upload)
	})

	logger.Info().Str("port", port).Str("bucket", bucket).Str("metadata", metadataURL).Msg("upload starting")
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}
