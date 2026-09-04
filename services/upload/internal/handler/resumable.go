package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ResumableStorage extends PresignStorage with size and object access.
type ResumableStorage interface {
	StatObjectSize(ctx context.Context, key string) (int64, error)
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	StatObject(ctx context.Context, key string) error
}

type ResumableHandler struct {
	storage ResumableStorage
}

func NewResumableHandler(s ResumableStorage) *ResumableHandler {
	return &ResumableHandler{storage: s}
}

// statusResponse for GET resumable offset.
type statusResponse struct {
	Uploaded int64 `json:"uploaded"`
	Total    *int64 `json:"total,omitempty"`
}

// Status godoc
// @Summary Get resumable upload offset
// @Description Returns uploaded bytes for raw/{id}/original.mp4. Used for Content-Range resume.
// @Tags upload
// @Produce json
// @Param id path string true "Video ID"
// @Success 200 {object} statusResponse
// @Router /api/v1/videos/{id}/resumable [get]
func (h *ResumableHandler) Status(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "id")
	if videoID == "" {
		http.Error(w, `{"error":"video_id required"}`, 400)
		return
	}
	key := fmt.Sprintf("raw/%s/original.mp4", videoID)
	size, err := h.storage.StatObjectSize(r.Context(), key)
	if err != nil {
		// not found -> 0 uploaded
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "The specified key does not exist") {
			size = 0
		} else if strings.Contains(strings.ToLower(err.Error()), "not found") {
			size = 0
		} else {
			// treat any stat error as 0 for resume (object not exist yet)
			// but check if it's truly not found by trying alternative: if err contains 404
			size = 0
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusResponse{Uploaded: size})
}

// Content-Range format: "bytes start-end/total" or "bytes start-end/*" or "bytes */total"
var crRe = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+|\*)$`)

func parseContentRange(v string) (start, end, total int64, hasTotal bool, err error) {
	v = strings.TrimSpace(v)
	m := crRe.FindStringSubmatch(v)
	if m == nil {
		return 0, 0, 0, false, fmt.Errorf("invalid Content-Range: %s", v)
	}
	start, _ = strconv.ParseInt(m[1], 10, 64)
	end, _ = strconv.ParseInt(m[2], 10, 64)
	if m[3] == "*" {
		return start, end, 0, false, nil
	}
	total, _ = strconv.ParseInt(m[3], 10, 64)
	return start, end, total, true, nil
}

// Upload godoc
// @Summary Resumable chunk upload via Content-Range
// @Description Accepts PUT with Content-Range: bytes start-end/total. Appends chunk to raw/{id}/original.mp4 on MinIO. Returns 308 Resume Incomplete until complete, 200 when done.
// @Tags upload
// @Param id path string true "Video ID"
// @Param Content-Range header string true "bytes 0-999/5000"
// @Success 200 {object} map[string]string
// @Success 308 {object} map[string]string
// @Router /api/v1/videos/{id}/resumable [put]
func (h *ResumableHandler) Upload(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "id")
	if videoID == "" {
		http.Error(w, `{"error":"video_id required"}`, 400)
		return
	}
	key := fmt.Sprintf("raw/%s/original.mp4", videoID)
	cr := r.Header.Get("Content-Range")
	if cr == "" {
		// No Content-Range -> treat as regular PUT (full file)
		// read body as complete object
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			ct = "video/mp4"
		}
		// limit size via ContentLength if available, else read with MaxBytesReader already at gateway
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"error":"read body"}`, 400)
			return
		}
		if err := h.storage.PutObject(r.Context(), key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
			http.Error(w, `{"error":"put: `+err.Error()+`"}`, 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "uploaded": strconv.Itoa(len(data))})
		return
	}
	start, end, total, hasTotal, err := parseContentRange(cr)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 400)
		return
	}
	if start < 0 || end < start {
		http.Error(w, `{"error":"invalid range"}`, 400)
		return
	}
	chunkSize := end - start + 1
	// content-length check is optional; chunked transfer may not set it
	// Get existing size
	existing, err := h.storage.StatObjectSize(r.Context(), key)
	if err != nil {
		existing = 0
	}
	if start != existing {
		// Range mismatch -> tell client current offset
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", existing-1))
		if existing == 0 {
			w.Header().Set("Range", "bytes=0-0")
		}
		http.Error(w, fmt.Sprintf(`{"error":"range mismatch: expected start %d got %d","uploaded":%d}`, existing, start, existing), http.StatusRequestedRangeNotSatisfiable)
		return
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp4"
	}
	chunk, err := io.ReadAll(io.LimitReader(r.Body, chunkSize+1))
	if err != nil {
		http.Error(w, `{"error":"read chunk"}`, 400)
		return
	}
	if int64(len(chunk)) != chunkSize {
		http.Error(w, fmt.Sprintf(`{"error":"chunk size mismatch: expected %d got %d"}`, chunkSize, len(chunk)), 400)
		return
	}
	// Append: if existing==0 just put, else get existing + append
	var toPut []byte
	if existing == 0 {
		toPut = chunk
	} else {
		rc, err := h.storage.GetObject(r.Context(), key)
		if err != nil {
			http.Error(w, `{"error":"get existing: `+err.Error()+`"}`, 500)
			return
		}
		existingData, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			http.Error(w, `{"error":"read existing"}`, 500)
			return
		}
		toPut = append(existingData, chunk...)
	}
	if err := h.storage.PutObject(r.Context(), key, bytes.NewReader(toPut), int64(len(toPut)), ct); err != nil {
		http.Error(w, `{"error":"put: `+err.Error()+`"}`, 500)
		return
	}
	uploaded := int64(len(toPut))
	if hasTotal && uploaded == total {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "complete", "uploaded": uploaded, "total": total})
		return
	}
	if hasTotal && uploaded < total {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", uploaded-1))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(308)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "resume", "uploaded": uploaded, "total": total})
		return
	}
	// no total -> 308
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", uploaded-1))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(308)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "resume", "uploaded": uploaded})
}

// Ensure interface compliance at compile time
var _ = regexp.MustCompile
