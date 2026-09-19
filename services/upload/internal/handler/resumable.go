package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	mw "flowix/pkg/middleware"
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
	owner   OwnershipChecker
}

func NewResumableHandler(s ResumableStorage, o OwnershipChecker) *ResumableHandler {
	return &ResumableHandler{storage: s, owner: o}
}

// statusResponse for GET resumable offset.
type statusResponse struct {
	Uploaded int64  `json:"uploaded"`
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
		writeError(w, r, http.StatusBadRequest, "video_id required", nil)
		return
	}
	// IDOR (issue #46): only the owner may read the upload offset.
	if !requireOwnership(h.owner, videoID, mw.UserIDFromCtx(r.Context()), w, r) {
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
	writeJSON(w, r, http.StatusOK, statusResponse{Uploaded: size})
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
		writeError(w, r, http.StatusBadRequest, "video_id required", nil)
		return
	}
	// IDOR (issue #46): only the owner may append/overwrite the raw object.
	if !requireOwnership(h.owner, videoID, mw.UserIDFromCtx(r.Context()), w, r) {
		return
	}
	maxBytes := int64(5 << 30) // 5GB
	if v := os.Getenv("UPLOAD_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxBytes = n
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	key := fmt.Sprintf("raw/%s/original.mp4", videoID)
	cr := r.Header.Get("Content-Range")
	if cr == "" {
		// No Content-Range -> treat as regular PUT (full file)
		// read body as complete object
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			ct = "video/mp4"
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				writeError(w, r, http.StatusRequestEntityTooLarge, "file too large (max "+strconv.FormatInt(maxBytes, 10)+" bytes)", err)
				return
			}
			writeError(w, r, http.StatusBadRequest, "read body", err)
			return
		}
		if err := h.storage.PutObject(r.Context(), key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
			writeError(w, r, http.StatusInternalServerError, "storage error", err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok", "uploaded": strconv.Itoa(len(data))})
		return
	}
	start, end, total, hasTotal, err := parseContentRange(cr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid Content-Range", err)
		return
	}
	if start < 0 || end < start {
		writeError(w, r, http.StatusBadRequest, "invalid range", nil)
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
		writeJSON(w, r, http.StatusRequestedRangeNotSatisfiable, map[string]interface{}{
			"error":    fmt.Sprintf("range mismatch: expected start %d got %d", existing, start),
			"uploaded": existing,
		})
		return
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp4"
	}
	if existing+chunkSize > maxBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "file too large (max "+strconv.FormatInt(maxBytes, 10)+" bytes)", nil)
		return
	}
	chunk, err := io.ReadAll(io.LimitReader(r.Body, chunkSize+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "file too large (max "+strconv.FormatInt(maxBytes, 10)+" bytes)", err)
			return
		}
		writeError(w, r, http.StatusBadRequest, "read chunk", err)
		return
	}
	if int64(len(chunk)) != chunkSize {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("chunk size mismatch: expected %d got %d", chunkSize, len(chunk)), nil)
		return
	}
	// Append: if existing==0 just put, else get existing + append
	var toPut []byte
	if existing == 0 {
		toPut = chunk
	} else {
		rc, err := h.storage.GetObject(r.Context(), key)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "get existing failed", err)
			return
		}
		existingData, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "read existing failed", err)
			return
		}
		toPut = append(existingData, chunk...)
	}
	if err := h.storage.PutObject(r.Context(), key, bytes.NewReader(toPut), int64(len(toPut)), ct); err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage error", err)
		return
	}
	uploaded := int64(len(toPut))
	if hasTotal && uploaded == total {
		writeJSON(w, r, http.StatusOK, map[string]interface{}{"status": "complete", "uploaded": uploaded, "total": total})
		return
	}
	if hasTotal && uploaded < total {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", uploaded-1))
		writeJSON(w, r, http.StatusPermanentRedirect, map[string]interface{}{"status": "resume", "uploaded": uploaded, "total": total})
		return
	}
	// no total -> 308
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", uploaded-1))
	writeJSON(w, r, http.StatusPermanentRedirect, map[string]interface{}{"status": "resume", "uploaded": uploaded})
}

// Ensure interface compliance at compile time
var _ = regexp.MustCompile
