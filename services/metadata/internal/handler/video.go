package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"flowix/metadata/internal/middleware"
	"flowix/metadata/internal/model"
	"flowix/metadata/internal/repository"
)

type VideoHandler struct {
	repo *repository.VideoRepo
}

func NewVideoHandler(repo *repository.VideoRepo) *VideoHandler { return &VideoHandler{repo: repo} }

func (h *VideoHandler) Register(r chi.Router) {
	// public: health
	// protected video routes
	r.Route("/api/v1/videos", func(r chi.Router) {
		r.Post("/", h.Create)
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Patch("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
	// internal (no auth) for transcoder/upload
	r.Patch("/internal/videos/{id}/status", h.UpdateStatus)
	r.Get("/internal/videos/{id}", h.GetInternal)
}

func (h *VideoHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID := middleware.UserIDFromCtx(r.Context())
	var req model.CreateVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}
	if req.Title == "" {
		http.Error(w, `{"error":"title required"}`, 400)
		return
	}
	v, err := h.repo.Create(r.Context(), ownerID, req.Title, req.Description)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(v)
}

func (h *VideoHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	v, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *VideoHandler) GetInternal(w http.ResponseWriter, r *http.Request) {
	// same as Get but for internal service-to-service (no auth)
	h.Get(w, r)
}

func (h *VideoHandler) List(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	limit := 20
	offset := 0
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
		offset = v
	}
	list, err := h.repo.List(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	if list == nil {
		list = []model.Video{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"data": list, "limit": limit, "offset": offset})
}

func (h *VideoHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID := middleware.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	var req model.UpdateVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}
	v, err := h.repo.Update(r.Context(), id, ownerID, req)
	if err != nil {
		if err.Error() == "forbidden" {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *VideoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID := middleware.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	err := h.repo.Delete(r.Context(), id, ownerID)
	if err != nil {
		if err.Error() == "forbidden" {
			http.Error(w, `{"error":"forbidden"}`, 403)
			return
		}
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	w.WriteHeader(204)
}

func (h *VideoHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req model.UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}
	if req.Status != model.StatusUploaded && req.Status != model.StatusProcessing && req.Status != model.StatusReady && req.Status != model.StatusFailed {
		http.Error(w, `{"error":"invalid status"}`, 400)
		return
	}
	if err := h.repo.UpdateStatus(r.Context(), id, req.Status, req.Renditions); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
