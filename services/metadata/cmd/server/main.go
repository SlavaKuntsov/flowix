package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	port := os.Getenv("METADATA_PORT")
	if port == "" {
		port = "8002"
	}
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "metadata"})
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"service": "metadata", "phase": "stub — implement CRUD after Phase 2b"})
	})
	log.Printf("metadata stub listening :%s", port)
	http.ListenAndServe(":"+port, r)
}
