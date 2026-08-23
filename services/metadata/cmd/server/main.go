package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"flowix/metadata/internal/handler"
	mw "flowix/metadata/internal/middleware"
	"flowix/metadata/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func main() {
	port := os.Getenv("METADATA_PORT")
	if port == "" {
		port = "8002"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://flowix:flowix@localhost:5432/flowix?sslmode=disable"
	}
	// pgx doesn't like sslmode, strip it for compat (same as auth)
	dbURL = stripSSLMode(dbURL)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "change-me-super-secret-jwt-key-32chars"
	}
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("db ping: %v", err)
	}
	logger.Info().Str("port", port).Msg("metadata starting")

	repo := repository.NewVideoRepo(pool)
	vh := handler.NewVideoHandler(repo)

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer, middleware.RequestID)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "metadata"})
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"service": "metadata", "version": "0.1"})
	})
	// protected routes: need JWT
	r.Group(func(r chi.Router) {
		r.Use(mw.AuthMiddleware(jwtSecret))
		r.Post("/api/v1/videos", vh.Create)
		r.Patch("/api/v1/videos/{id}", vh.Update)
		r.Delete("/api/v1/videos/{id}", vh.Delete)
	})
	// public list/get
	r.Get("/api/v1/videos", vh.List)
	r.Get("/api/v1/videos/{id}", vh.Get)
	// internal (no auth)
	r.Patch("/internal/videos/{id}/status", vh.UpdateStatus)
	r.Get("/internal/videos/{id}", vh.GetInternal)

	log.Printf("metadata listening :%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

func stripSSLMode(s string) string {
	if !strings.Contains(s, "sslmode=") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	q := u.Query()
	q.Del("sslmode")
	u.RawQuery = q.Encode()
	return u.String()
}
