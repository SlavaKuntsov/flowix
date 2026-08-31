package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"flowix/upload/internal/metrics"
	mw "flowix/upload/internal/middleware"
)

// UploadHandler dependencies as interfaces — T3 stepwise fakes without MinIO/RabbitMQ.
type Storage interface {
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
}

type Publisher interface {
	Publish(ctx context.Context, payload interface{}) error
}

type MetadataCreator interface {
	CreateVideo(token, title, description string) (string, error)
}

type UploadHandler struct {
	storage   Storage
	publisher Publisher
	metadata  MetadataCreator
}

type VideoUploadedEvent struct {
	VideoID string `json:"video_id"`
	S3Key   string `json:"s3_key"`
	OwnerID string `json:"owner_id"`
}

func NewUploadHandler(s Storage, p Publisher, m MetadataCreator) *UploadHandler {
	return &UploadHandler{storage: s, publisher: p, metadata: m}
}

// Upload godoc
// @Summary Upload video file
// @Description Multipart upload: file + title/description. Creates metadata, stores to MinIO, publishes video.uploaded.
// @Tags upload
// @Accept mpfd
// @Produce json
// @Param file formData file true "video file"
// @Param title formData string false "Title (default filename)"
// @Param description formData string false "Description"
// @Security BearerAuth
// @Success 201 {object} map[string]interface{} "id, s3_key, status"
// @Failure 400 {string} string "invalid body"
// @Failure 401 {string} string "unauthorized"
// @Failure 502 {string} string "metadata error"
// @Router /api/v1/videos/upload [post]
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	ownerID := mw.UserIDFromCtx(r.Context())
	authHeader := r.Header.Get("Authorization")
	token := ""
	if len(authHeader) > 7 {
		token = authHeader[7:]
	}

	// 5-6GB limit for Phase 9 (env UPLOAD_MAX_BYTES, default 5GB). Streaming, not buffering whole file.
	maxBytes := int64(5 << 30) // 5GB
	if v := os.Getenv("UPLOAD_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxBytes = n
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	// Keep memory low — rest goes to temp files (os.TempDir)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, `{"error":"file too large (max `+strconv.FormatInt(maxBytes, 10)+` bytes)"}`, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, `{"error":"parse form: `+err.Error()+`"}`, 400)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file required (field 'file')"}`, 400)
		return
	}
	defer func() { _ = file.Close() }()

	// Minimal MIME validation — header can be spoofed, deep check via ffprobe is in transcoder
	if ct := header.Header.Get("Content-Type"); ct != "" && !isAllowedContentType(ct) {
		http.Error(w, `{"error":"unsupported content type: `+ct+`"}`, 400)
		return
	}

	title := r.FormValue("title")
	if title == "" {
		title = header.Filename
	}
	if title == "" {
		title = "Untitled"
	}
	description := r.FormValue("description")

	// 1. create metadata record first to get video_id
	videoID, err := h.metadata.CreateVideo(token, title, description)
	if err != nil {
		http.Error(w, `{"error":"metadata: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}

	s3Key := fmt.Sprintf("raw/%s/original.mp4", videoID)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "video/mp4"
	}

	// 2. upload to MinIO — size from header may be -1, stream correctly
	if err := h.storage.PutObject(r.Context(), s3Key, file, header.Size, contentType); err != nil {
		// MaxBytesReader returns error on too large body
		if strings.Contains(err.Error(), "http: request body too large") {
			http.Error(w, `{"error":"file too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, `{"error":"storage: `+err.Error()+`"}`, 500)
		return
	}

	if header.Size > 0 {
		metrics.UploadBytes.Add(float64(header.Size))
	}
	// 3. publish event
	ev := VideoUploadedEvent{VideoID: videoID, S3Key: s3Key, OwnerID: ownerID}
	if err := h.publisher.Publish(r.Context(), ev); err != nil {
		// log but don't fail upload — transcoder will be triggered manually if needed
		// for MVP return 500 so client knows
		http.Error(w, `{"error":"queue: `+err.Error()+`"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     videoID,
		"s3_key": s3Key,
		"status": "uploaded",
	})
}

var allowedCT = map[string]bool{
	"video/mp4": true, "video/quicktime": true, "video/x-matroska": true, "video/webm": true,
	"video/avi": true, "video/mov": true, "application/octet-stream": true,
}

func isAllowedContentType(ct string) bool {
	// strip params like "; charset=utf-8"
	if i := strings.Index(ct, ";"); i != -1 {
		ct = strings.TrimSpace(ct[:i])
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	if allowedCT[ct] {
		return true
	}
	// allow any video/*
	return strings.HasPrefix(ct, "video/")
}
