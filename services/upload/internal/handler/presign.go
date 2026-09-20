package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"flowix/pkg/metrics"
	mw "flowix/pkg/middleware"
	"github.com/go-chi/chi/v5"
)

// PresignStorage generates presigned URLs and checks existence.
type PresignStorage interface {
	PresignedPutObjectExternal(ctx context.Context, key string, expires time.Duration, publicEndpoint string) (string, error)
	StatObject(ctx context.Context, key string) error
	StatObjectInfo(ctx context.Context, key string) (int64, string, error)
}

// We use context.Context but avoid import cycle; interface matches storage.
type presignRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type presignResponse struct {
	ID        string `json:"id"`
	VideoID   string `json:"video_id"`
	S3Key     string `json:"s3_key"`
	Method    string `json:"method"`
	URL       string `json:"url"`
	ExpiresIn int    `json:"expires_in"`
}

type PresignHandler struct {
	storage   PresignStorage
	publisher Publisher
	metadata  MetadataCreator
	owner     OwnershipChecker
}

func NewPresignHandler(s PresignStorage, p Publisher, m MetadataCreator, o OwnershipChecker) *PresignHandler {
	return &PresignHandler{storage: s, publisher: p, metadata: m, owner: o}
}

// Presign godoc
// @Summary Presign upload via direct MinIO PUT
// @Description Creates metadata record and returns presigned PUT URL for raw/{id}/original.mp4. Client PUTs directly to MinIO, then calls complete.
// @Tags upload
// @Accept json
// @Produce json
// @Param body body presignRequest true "Presign request"
// @Security BearerAuth
// @Success 201 {object} presignResponse
// @Router /api/v1/videos/presign [post]
func (h *PresignHandler) Presign(w http.ResponseWriter, r *http.Request) {
	ownerID := mw.UserIDFromCtx(r.Context())
	authHeader := r.Header.Get("Authorization")
	token := ""
	if len(authHeader) > 7 {
		token = authHeader[7:]
	}
	var req presignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body", nil)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = strings.TrimSpace(req.Filename)
	}
	if title == "" {
		title = "Untitled"
	}
	ct := strings.TrimSpace(req.ContentType)
	if ct != "" && !isAllowedContentType(ct) {
		writeError(w, r, http.StatusBadRequest, "unsupported content type: "+ct, nil)
		return
	}
	// create metadata first
	videoID, err := h.metadata.CreateVideo(token, title, req.Description)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "metadata unavailable", err)
		return
	}
	_ = ownerID // owner stored in metadata via token
	s3Key := fmt.Sprintf("raw/%s/original.mp4", videoID)
	publicEndpoint := os.Getenv("MINIO_PUBLIC_ENDPOINT")
	if publicEndpoint == "" {
		// fallback to external MinIO URL for presign; browser needs localhost:9000
		// MINIO_PUBLIC_ENDPOINT should be http://localhost:9000 in .env.example
		publicEndpoint = os.Getenv("MINIO_EXTERNAL_URL")
	}
	expires := time.Hour
	urlStr, err := h.storage.PresignedPutObjectExternal(r.Context(), s3Key, expires, publicEndpoint)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "presign failed", err)
		return
	}
	writeJSON(w, r, http.StatusCreated, presignResponse{
		ID:        videoID,
		VideoID:   videoID,
		S3Key:     s3Key,
		Method:    "PUT",
		URL:       urlStr,
		ExpiresIn: int(expires.Seconds()),
	})
}

// Complete godoc
// @Summary Complete presigned upload
// @Description Verifies raw object exists in MinIO, then publishes video.uploaded event. Idempotent.
// @Tags upload
// @Accept json
// @Produce json
// @Param id path string true "Video ID"
// @Security BearerAuth
// @Success 200 {object} map[string]string
// @Router /api/v1/videos/{id}/complete [post]
func (h *PresignHandler) Complete(w http.ResponseWriter, r *http.Request) {
	ownerID := mw.UserIDFromCtx(r.Context())
	videoID := chi.URLParam(r, "id")
	if videoID == "" {
		// fallback to body {video_id}
		var body struct {
			VideoID string `json:"video_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			videoID = body.VideoID
		}
	}
	if videoID == "" {
		writeError(w, r, http.StatusBadRequest, "video_id required", nil)
		return
	}
	// IDOR (issue #46): only the owner may complete an upload for a video id.
	if !requireOwnership(h.owner, videoID, ownerID, w, r) {
		return
	}
	s3Key := fmt.Sprintf("raw/%s/original.mp4", videoID)
	// Verify the uploaded object matches a real video PUT (issue #46).
	size, contentType, err := h.storage.StatObjectInfo(r.Context(), s3Key)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "object not found, upload via presigned URL first", nil)
		return
	}
	if size == 0 {
		writeError(w, r, http.StatusBadRequest, "empty object", nil)
		return
	}
	if contentType != "" && !strings.HasPrefix(contentType, "video/") {
		writeError(w, r, http.StatusBadRequest, "unexpected content type: "+contentType, nil)
		return
	}
	metrics.UploadBytes.Inc()
	ev := VideoUploadedEvent{VideoID: videoID, S3Key: s3Key, OwnerID: ownerID}
	if err := h.publisher.Publish(r.Context(), ev); err != nil {
		writeError(w, r, http.StatusInternalServerError, "queue error", err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]interface{}{
		"id":     videoID,
		"s3_key": s3Key,
		"status": "uploaded",
	})
}
