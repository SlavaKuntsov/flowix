package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

	// 10GB limit for MVP, but protect memory
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		http.Error(w, `{"error":"parse form: `+err.Error()+`"}`, 400)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file required (field 'file')"}`, 400)
		return
	}
	defer func() { _ = file.Close() }()

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

	// 2. upload to MinIO
	if err := h.storage.PutObject(r.Context(), s3Key, file, header.Size, contentType); err != nil {
		http.Error(w, `{"error":"storage: `+err.Error()+`"}`, 500)
		return
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
