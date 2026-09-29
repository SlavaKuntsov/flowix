package handler

// S3 multipart upload orchestration (issue #56): replaces the O(n²)
// Content-Range append — the browser PUTs presigned 8MB parts directly to
// MinIO, the server only creates the session, presigns parts and completes.

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
	"flowix/upload/internal/storage"

	"github.com/go-chi/chi/v5"
)

// S3 requires every part except the last to be ≥5MB — 8MB sits in the
// 5-10MB window the issue asks for.
const MultipartChunkSize = 8 << 20

// maxPartNumber is the S3 protocol limit (10000 parts per upload).
const maxPartNumber = 10000

// MultipartStorage orchestrates S3 multipart sessions.
type MultipartStorage interface {
	CreateMultipartUpload(ctx context.Context, key, contentType string) (string, error)
	FindUploadID(ctx context.Context, key string) (string, error)
	ListParts(ctx context.Context, key, uploadID string) ([]storage.MultipartPart, error)
	CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []storage.MultipartPart) (int64, error)
	PresignPart(ctx context.Context, key, uploadID string, partNumber int, expires time.Duration, publicEndpoint string) (string, error)
}

type MultipartHandler struct {
	storage   MultipartStorage
	publisher Publisher
	metadata  MetadataCreator
	owner     OwnershipChecker
}

func NewMultipartHandler(s MultipartStorage, p Publisher, m MetadataCreator, o OwnershipChecker) *MultipartHandler {
	return &MultipartHandler{storage: s, publisher: p, metadata: m, owner: o}
}

func multipartKey(videoID string) string {
	return fmt.Sprintf("raw/%s/original.mp4", videoID)
}

// publicEndpointFromEnv mirrors Presign: browser-visible MinIO host.
func publicEndpointFromEnv() string {
	if e := os.Getenv("MINIO_PUBLIC_ENDPOINT"); e != "" {
		return e
	}
	return os.Getenv("MINIO_EXTERNAL_URL")
}

type multipartCreateResponse struct {
	ID        string `json:"id"`
	VideoID   string `json:"video_id"`
	S3Key     string `json:"s3_key"`
	UploadID  string `json:"upload_id"`
	ChunkSize int    `json:"chunk_size"`
}

// Create godoc
// @Summary Create S3 multipart upload
// @Description Creates metadata record and starts a multipart session for raw/{id}/original.mp4. Presign parts next, then call complete.
// @Tags upload
// @Accept json
// @Produce json
// @Param body body presignRequest true "Presign request"
// @Security BearerAuth
// @Success 201 {object} multipartCreateResponse
// @Router /api/v1/videos/multipart [post]
func (h *MultipartHandler) Create(w http.ResponseWriter, r *http.Request) {
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
	videoID, err := h.metadata.CreateVideo(token, title, req.Description)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "metadata unavailable", err)
		return
	}
	key := multipartKey(videoID)
	uploadID, err := h.storage.CreateMultipartUpload(r.Context(), key, ct)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "create multipart upload failed", err)
		return
	}
	writeJSON(w, r, http.StatusCreated, multipartCreateResponse{
		ID:        videoID,
		VideoID:   videoID,
		S3Key:     key,
		UploadID:  uploadID,
		ChunkSize: MultipartChunkSize,
	})
}

type multipartPartsResponse struct {
	VideoID   string                  `json:"video_id"`
	UploadID  string                  `json:"upload_id"`
	ChunkSize int                     `json:"chunk_size"`
	Parts     []storage.MultipartPart `json:"parts"`
}

// Parts godoc
// @Summary List uploaded multipart parts
// @Description Returns the in-progress upload parts for resume. upload_id is resolved server-side.
// @Tags upload
// @Produce json
// @Param id path string true "Video ID"
// @Success 200 {object} multipartPartsResponse
// @Router /api/v1/videos/{id}/multipart [get]
func (h *MultipartHandler) Parts(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "id")
	if videoID == "" {
		writeError(w, r, http.StatusBadRequest, "video_id required", nil)
		return
	}
	if !requireOwnership(h.owner, videoID, mw.UserIDFromCtx(r.Context()), w, r) {
		return
	}
	key := multipartKey(videoID)
	uploadID, err := h.storage.FindUploadID(r.Context(), key)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "multipart upload not found", err)
		return
	}
	parts, err := h.storage.ListParts(r.Context(), key, uploadID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "list parts failed", err)
		return
	}
	writeJSON(w, r, http.StatusOK, multipartPartsResponse{
		VideoID:   videoID,
		UploadID:  uploadID,
		ChunkSize: MultipartChunkSize,
		Parts:     parts,
	})
}

type presignPartRequest struct {
	PartNumber int `json:"part_number"`
}

type presignPartResponse struct {
	PartNumber int    `json:"part_number"`
	URL        string `json:"url"`
	ExpiresIn  int    `json:"expires_in"`
}

// PresignPart godoc
// @Summary Presign a single multipart part
// @Description Returns a presigned PUT URL for one part; the browser uploads the chunk directly to MinIO.
// @Tags upload
// @Accept json
// @Produce json
// @Param id path string true "Video ID"
// @Param body body presignPartRequest true "Part number"
// @Security BearerAuth
// @Success 200 {object} presignPartResponse
// @Router /api/v1/videos/{id}/multipart/presign-part [post]
func (h *MultipartHandler) PresignPart(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "id")
	if videoID == "" {
		writeError(w, r, http.StatusBadRequest, "video_id required", nil)
		return
	}
	if !requireOwnership(h.owner, videoID, mw.UserIDFromCtx(r.Context()), w, r) {
		return
	}
	var req presignPartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid body", nil)
		return
	}
	if req.PartNumber < 1 || req.PartNumber > maxPartNumber {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("part_number must be in [1,%d]", maxPartNumber), nil)
		return
	}
	key := multipartKey(videoID)
	uploadID, err := h.storage.FindUploadID(r.Context(), key)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "multipart upload not found", err)
		return
	}
	expires := time.Hour
	urlStr, err := h.storage.PresignPart(r.Context(), key, uploadID, req.PartNumber, expires, publicEndpointFromEnv())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "presign part failed", err)
		return
	}
	writeJSON(w, r, http.StatusOK, presignPartResponse{
		PartNumber: req.PartNumber,
		URL:        urlStr,
		ExpiresIn:  int(expires.Seconds()),
	})
}

// Complete godoc
// @Summary Complete S3 multipart upload
// @Description Lists uploaded parts server-side, completes the multipart session and publishes video.uploaded. Idempotent until the session is gone.
// @Tags upload
// @Param id path string true "Video ID"
// @Security BearerAuth
// @Success 200 {object} map[string]string
// @Router /api/v1/videos/{id}/multipart/complete [post]
func (h *MultipartHandler) Complete(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "id")
	if videoID == "" {
		writeError(w, r, http.StatusBadRequest, "video_id required", nil)
		return
	}
	ownerID := mw.UserIDFromCtx(r.Context())
	if !requireOwnership(h.owner, videoID, ownerID, w, r) {
		return
	}
	key := multipartKey(videoID)
	uploadID, err := h.storage.FindUploadID(r.Context(), key)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "multipart upload not found", err)
		return
	}
	parts, err := h.storage.ListParts(r.Context(), key, uploadID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "list parts failed", err)
		return
	}
	if len(parts) == 0 {
		writeError(w, r, http.StatusBadRequest, "no parts uploaded", nil)
		return
	}
	if _, err := h.storage.CompleteMultipartUpload(r.Context(), key, uploadID, parts); err != nil {
		writeError(w, r, http.StatusInternalServerError, "complete multipart upload failed", err)
		return
	}
	metrics.UploadBytes.Inc()
	ev := VideoUploadedEvent{VideoID: videoID, S3Key: key, OwnerID: ownerID}
	if err := h.publisher.Publish(r.Context(), ev); err != nil {
		writeError(w, r, http.StatusInternalServerError, "queue error", err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]interface{}{
		"id":     videoID,
		"s3_key": key,
		"status": "uploaded",
	})
}
