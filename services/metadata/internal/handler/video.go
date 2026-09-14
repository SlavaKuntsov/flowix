package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"flowix/metadata/internal/middleware"
	"flowix/metadata/internal/model"

	"github.com/go-chi/chi/v5"
)

// VideoStore abstracts persistence — allows in-memory fake for tests (T2 stepwise).
type VideoStore interface {
	Create(ctx context.Context, ownerID, title, description string) (*model.Video, error)
	GetByID(ctx context.Context, id string) (*model.Video, error)
	List(ctx context.Context, limit, offset int, viewerID string) ([]model.Video, error)
	Update(ctx context.Context, id, ownerID string, req model.UpdateVideoRequest) (*model.Video, error)
	Delete(ctx context.Context, id, ownerID string) error
	UpdateStatus(ctx context.Context, id string, status model.VideoStatus, renditions []model.Rendition) error
	UpdateThumbnail(ctx context.Context, id string, thumbnailS3Key string) error
}

// StorageRemover deletes S3 objects (raw + renditions + thumbnail). Nil = skip (tests).
type StorageRemover interface {
	RemoveObjects(ctx context.Context, keys []string)
	RemovePrefix(ctx context.Context, prefix string)
}

// StoragePresigner generates presigned GET URLs (issue #43: bucket fully private —
// nginx-vod upstream fetches and browsers need signed reads). Optional capability
// of the storage implementation, detected via type assertion.
type StoragePresigner interface {
	PresignGetInternal(ctx context.Context, key string, expiry time.Duration) (string, error)
	PresignGetPublic(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// presignExpiry is the SigV4 presign cap (7 days) — far above any mapping/thumbnail cache TTL.
const presignExpiry = 168 * time.Hour

type VideoHandler struct {
	repo    VideoStore
	storage StorageRemover
}

func NewVideoHandler(repo VideoStore) *VideoHandler { return &VideoHandler{repo: repo} }

// NewVideoHandlerWithStorage is used in prod to also clean MinIO.
func NewVideoHandlerWithStorage(repo VideoStore, s StorageRemover) *VideoHandler {
	return &VideoHandler{repo: repo, storage: s}
}

// presignThumbnail replaces the gateway-relative thumbnail_url with a presigned
// absolute URL (issue #43: MinIO no longer serves thumbnails anonymously).
func (h *VideoHandler) presignThumbnail(ctx context.Context, v *model.Video) {
	if h.storage == nil || v == nil || v.ThumbnailS3Key == nil || *v.ThumbnailS3Key == "" {
		return
	}
	ps, ok := h.storage.(StoragePresigner)
	if !ok {
		return
	}
	u, err := ps.PresignGetPublic(ctx, *v.ThumbnailS3Key, presignExpiry)
	if err != nil {
		slog.Warn("thumbnail presign failed", "error", err, "video_id", v.ID)
		return
	}
	v.ThumbnailURL = &u
}

func (h *VideoHandler) presignThumbnails(ctx context.Context, videos []model.Video) {
	for i := range videos {
		h.presignThumbnail(ctx, &videos[i])
	}
}

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
	r.Get("/internal/videos/{id}/vod", h.GetVODMapping)
}

// Create godoc
// @Summary Create video metadata
// @Tags videos
// @Accept json
// @Produce json
// @Param body body model.CreateVideoRequest true "Create"
// @Security BearerAuth
// @Success 201 {object} model.Video
// @Failure 400 {string} string "invalid body"
// @Failure 401 {string} string "unauthorized"
// @Router /api/v1/videos [post]
func (h *VideoHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID := middleware.UserIDFromCtx(r.Context())
	var req model.CreateVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Title == "" {
		writeError(w, r, http.StatusBadRequest, "title required")
		return
	}
	v, err := h.repo.Create(r.Context(), ownerID, req.Title, req.Description)
	if err != nil {
		slog.Error("create video failed", "error", err, "owner_id", ownerID)
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if req.Visibility != nil {
		if !req.Visibility.Valid() {
			writeError(w, r, http.StatusBadRequest, "invalid visibility")
			return
		}
		if upd, err := h.repo.Update(r.Context(), v.ID, ownerID, model.UpdateVideoRequest{Visibility: req.Visibility}); err == nil {
			v = upd
		}
	}
	h.presignThumbnail(r.Context(), v)
	writeJSON(w, r, http.StatusCreated, v)
}

// Get godoc
// @Summary Get video by ID
// @Tags videos
// @Produce json
// @Param id path string true "Video ID"
// @Success 200 {object} model.Video
// @Failure 403 {string} string "forbidden (private, not owner)"
// @Failure 404 {string} string "not found"
// @Router /api/v1/videos/{id} [get]
func (h *VideoHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.getVideo(w, r, true)
}

// GetInternal godoc
// @Summary Internal get video (no auth)
// @Tags internal
// @Produce json
// @Param id path string true "Video ID"
// @Success 200 {object} model.Video
// @Router /internal/videos/{id} [get]
func (h *VideoHandler) GetInternal(w http.ResponseWriter, r *http.Request) {
	h.getVideo(w, r, false)
}

// getVideo fetches one video; when enforceVisibility, a private video is
// returned only to its owner (issue #44) — unlisted stays reachable by link.
func (h *VideoHandler) getVideo(w http.ResponseWriter, r *http.Request, enforceVisibility bool) {
	id := chi.URLParam(r, "id")
	v, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	if enforceVisibility && v.Visibility == model.VisibilityPrivate && middleware.UserIDFromHeader(r) != v.OwnerID {
		writeError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	h.presignThumbnail(r.Context(), v)
	writeJSON(w, r, http.StatusOK, v)
}

type vodMapping struct {
	Sequences []vodSequence `json:"sequences"`
}

type vodSequence struct {
	Clips []vodClip `json:"clips"`
}

type vodClip struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// GetVODMapping returns the nginx-vod mapped-mode representation for a ready video.
// Phase 12: adaptive ladder — accept 1..3 renditions (not fixed 3).
func (h *VideoHandler) GetVODMapping(w http.ResponseWriter, r *http.Request) {
	v, err := h.repo.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil || v.Status != model.StatusReady || len(v.Renditions) == 0 {
		writeError(w, r, http.StatusNotFound, "video not ready")
		return
	}

	rends := append([]model.Rendition(nil), v.Renditions...)
	sort.Slice(rends, func(i, j int) bool { return rends[i].Bitrate < rends[j].Bitrate })
	mapping := vodMapping{Sequences: make([]vodSequence, 0, len(rends))}
	for _, rendition := range rends {
		// Issue #43: bucket is fully private — nginx-vod fetches MP4s through its
		// internal /minio/ location with a presigned query (SigV4 host = minio:9000).
		path := ""
		ps, hasPresigner := h.storage.(StoragePresigner)
		if !hasPresigner {
			// no MinIO storage wired (tests/local dev) — unsigned relative key
			path = "/" + rendition.S3Key
		} else {
			u, err := ps.PresignGetInternal(r.Context(), rendition.S3Key, presignExpiry)
			if err != nil {
				// unsigned path would 403 at MinIO — fail fast with a clear status
				slog.Error("rendition presign failed", "error", err, "key", rendition.S3Key)
				writeError(w, r, http.StatusBadGateway, "presign failed")
				return
			}
			if pu, perr := url.Parse(u); perr == nil && pu.RawQuery != "" {
				path = "/" + rendition.S3Key + "?" + pu.RawQuery
			} else {
				path = "/" + rendition.S3Key
			}
		}
		mapping.Sequences = append(mapping.Sequences, vodSequence{Clips: []vodClip{{
			Type: "source",
			Path: path,
		}}})
	}
	writeJSON(w, r, http.StatusOK, mapping)
}

// List godoc
// @Summary List videos
// @Tags videos
// @Produce json
// @Param limit query int false "Limit 1-100" default(20)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/videos [get]
func (h *VideoHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := 20
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	list, err := h.repo.List(r.Context(), limit, offset, middleware.UserIDFromHeader(r))
	if err != nil {
		slog.Error("list videos failed", "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if list == nil {
		list = []model.Video{}
	}
	h.presignThumbnails(r.Context(), list)
	writeJSON(w, r, http.StatusOK, map[string]interface{}{"data": list, "limit": limit, "offset": offset})
}

// Update godoc
// @Summary Update video (owner only)
// @Tags videos
// @Accept json
// @Produce json
// @Param id path string true "Video ID"
// @Param body body model.UpdateVideoRequest true "Update"
// @Security BearerAuth
// @Success 200 {object} model.Video
// @Failure 403 {string} string "forbidden"
// @Failure 404 {string} string "not found"
// @Router /api/v1/videos/{id} [patch]
func (h *VideoHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID := middleware.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	var req model.UpdateVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Visibility != nil && !req.Visibility.Valid() {
		writeError(w, r, http.StatusBadRequest, "invalid visibility")
		return
	}
	v, err := h.repo.Update(r.Context(), id, ownerID, req)
	if err != nil {
		if err.Error() == "forbidden" {
			writeError(w, r, http.StatusForbidden, "forbidden")
			return
		}
		if err.Error() == "invalid visibility" {
			writeError(w, r, http.StatusBadRequest, "invalid visibility")
			return
		}
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	h.presignThumbnail(r.Context(), v)
	writeJSON(w, r, http.StatusOK, v)
}

// Delete godoc
// @Summary Delete video (owner only)
// @Tags videos
// @Security BearerAuth
// @Param id path string true "Video ID"
// @Success 204 "No Content"
// @Failure 403 {string} string "forbidden"
// @Failure 404 {string} string "not found"
// @Router /api/v1/videos/{id} [delete]
func (h *VideoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID := middleware.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	// fetch first for S3 keys (need renditions + thumbnail before DB delete)
	var keys []string
	if h.storage != nil {
		if v, err := h.repo.GetByID(r.Context(), id); err == nil {
			keys = append(keys, "raw/"+id+"/original.mp4")
			for _, rn := range v.Renditions {
				if rn.S3Key != "" {
					keys = append(keys, rn.S3Key)
				}
			}
			if v.ThumbnailS3Key != nil && *v.ThumbnailS3Key != "" {
				keys = append(keys, *v.ThumbnailS3Key)
			} else {
				keys = append(keys, "thumbnails/"+id+"/thumb.jpg")
			}
		}
	}
	if err := h.repo.Delete(r.Context(), id, ownerID); err != nil {
		if err.Error() == "forbidden" {
			writeError(w, r, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	if h.storage != nil {
		if len(keys) > 0 {
			h.storage.RemoveObjects(r.Context(), keys)
		}
		// Also clean prefix orphans (e.g., video deleted while transcoding — renditions not yet in DB)
		h.storage.RemovePrefix(r.Context(), "renditions/"+id+"/")
		h.storage.RemovePrefix(r.Context(), "raw/"+id+"/")
		h.storage.RemovePrefix(r.Context(), "thumbnails/"+id+"/")
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateStatus godoc
// @Summary Internal update status (transcoder/upload)
// @Tags internal
// @Accept json
// @Produce json
// @Param id path string true "Video ID"
// @Param body body model.UpdateStatusRequest true "Status"
// @Success 200 {object} map[string]string
// @Router /internal/videos/{id}/status [patch]
func (h *VideoHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req model.UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Status != model.StatusUploaded && req.Status != model.StatusProcessing && req.Status != model.StatusReady && req.Status != model.StatusFailed {
		writeError(w, r, http.StatusBadRequest, "invalid status")
		return
	}
	if err := h.repo.UpdateStatus(r.Context(), id, req.Status, req.Renditions); err != nil {
		slog.Error("update status failed", "error", err, "video_id", id)
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	// optional thumbnail (sent by transcoder)
	thumbKey := ""
	if req.ThumbnailS3Key != nil {
		thumbKey = *req.ThumbnailS3Key
	} else if req.ThumbnailURL != nil {
		thumbKey = *req.ThumbnailURL
	}
	if thumbKey != "" {
		if err := h.repo.UpdateThumbnail(r.Context(), id, thumbKey); err != nil {
			slog.Error("update thumbnail failed", "error", err, "video_id", id)
		}
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}
