package handler

// Tests for S3 multipart orchestration (issue #56).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	mw "flowix/pkg/middleware"
	"flowix/upload/internal/storage"

	"github.com/go-chi/chi/v5"
)

type fakeMultipartStorage struct {
	createKey    string
	createCT     string
	createErr    error
	uploadID     string
	findErr      error
	parts        []storage.MultipartPart
	listErr      error
	completeErr  error
	completeSize int64
	statCT       string
	statErr      error
	presignedNum int
	presignedKey string
	presignErr   error
}

func (f *fakeMultipartStorage) CreateMultipartUpload(_ context.Context, key, contentType string) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createKey = key
	f.createCT = contentType
	if f.uploadID == "" {
		return "upload-1", nil
	}
	return f.uploadID, nil
}

func (f *fakeMultipartStorage) FindUploadID(_ context.Context, _ string) (string, error) {
	if f.findErr != nil {
		return "", f.findErr
	}
	if f.uploadID == "" {
		return "upload-1", nil
	}
	return f.uploadID, nil
}

func (f *fakeMultipartStorage) ListParts(_ context.Context, _, _ string) ([]storage.MultipartPart, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.parts, nil
}

func (f *fakeMultipartStorage) CompleteMultipartUpload(_ context.Context, _, _ string, parts []storage.MultipartPart) (int64, error) {
	if f.completeErr != nil {
		return 0, f.completeErr
	}
	if len(parts) == 0 {
		return 0, errors.New("no parts")
	}
	return f.completeSize, nil
}

func (f *fakeMultipartStorage) StatObjectInfo(_ context.Context, _ string) (int64, string, error) {
	if f.statErr != nil {
		return 0, "", f.statErr
	}
	if f.completeSize == 0 {
		return 12 << 20, "video/mp4", nil
	}
	return f.completeSize, f.statCT, nil
}

func (f *fakeMultipartStorage) PresignPart(_ context.Context, key, _ string, partNumber int, _ time.Duration, _ string) (string, error) {
	if f.presignErr != nil {
		return "", f.presignErr
	}
	f.presignedNum = partNumber
	f.presignedKey = key
	return "http://localhost:9000/videos/" + key + "?partNumber=" + strconv.Itoa(partNumber), nil
}

func newMultipartRouter(h *MultipartHandler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/videos/multipart", h.Create)
	r.Get("/api/v1/videos/{id}/multipart", h.Parts)
	r.Post("/api/v1/videos/{id}/multipart/presign-part", h.PresignPart)
	r.Post("/api/v1/videos/{id}/multipart/complete", h.Complete)
	return r
}

func withAuth(req *http.Request, userID string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token(userID))
	return req.WithContext(context.WithValue(req.Context(), mw.UserIDKey, userID))
}

func TestMultipartCreateSuccess(t *testing.T) {
	meta := &fakeMeta{id: "vid-123"}
	st := &fakeMultipartStorage{uploadID: "up-1"}
	h := NewMultipartHandler(st, &fakePub{}, meta, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)

	body, _ := json.Marshal(map[string]string{"title": "hello", "filename": "test.mp4", "content_type": "video/mp4"})
	req := httptest.NewRequest("POST", "/api/v1/videos/multipart", bytes.NewReader(body))
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("want 201 got %d body %s", w.Code, w.Body.String())
	}
	var out multipartCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.VideoID != "vid-123" || out.UploadID != "up-1" || out.S3Key != "raw/vid-123/original.mp4" {
		t.Fatalf("bad response %+v", out)
	}
	if out.ChunkSize != MultipartChunkSize {
		t.Fatalf("chunk size %d", out.ChunkSize)
	}
	if st.createKey != "raw/vid-123/original.mp4" || st.createCT != "video/mp4" {
		t.Fatalf("bad create args key=%s ct=%s", st.createKey, st.createCT)
	}
}

func TestMultipartCreateMetadataUnavailable(t *testing.T) {
	h := NewMultipartHandler(
		&fakeMultipartStorage{},
		&fakePub{},
		&fakeMeta{err: errFake("connection refused")},
		&fakeOwnership{owner: "owner1"},
	)
	r := newMultipartRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/multipart", bytes.NewReader([]byte(`{}`)))
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 502 {
		t.Fatalf("want 502 got %d %s", w.Code, w.Body.String())
	}
}

func TestMultipartPartsResume(t *testing.T) {
	st := &fakeMultipartStorage{
		uploadID: "up-1",
		parts: []storage.MultipartPart{
			{PartNumber: 1, Size: 8 << 20, ETag: "etag1"},
			{PartNumber: 2, Size: 8 << 20, ETag: "etag2"},
		},
	}
	h := NewMultipartHandler(st, &fakePub{}, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("GET", "/api/v1/videos/vid-1/multipart", nil)
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	var out multipartPartsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Parts) != 2 || out.UploadID != "up-1" {
		t.Fatalf("bad parts response %+v", out)
	}
}

func TestMultipartPartsForeignVideoForbidden(t *testing.T) {
	h := NewMultipartHandler(&fakeMultipartStorage{}, &fakePub{}, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "real-owner"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("GET", "/api/v1/videos/vid-1/multipart", nil)
	req = withAuth(req, "attacker")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d %s", w.Code, w.Body.String())
	}
}

func TestMultipartPartsNoUpload(t *testing.T) {
	st := &fakeMultipartStorage{findErr: errFake("multipart upload not found for raw/vid-1/original.mp4")}
	h := NewMultipartHandler(st, &fakePub{}, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("GET", "/api/v1/videos/vid-1/multipart", nil)
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("want 404 got %d %s", w.Code, w.Body.String())
	}
}

func TestMultipartPresignPartSuccess(t *testing.T) {
	st := &fakeMultipartStorage{}
	h := NewMultipartHandler(st, &fakePub{}, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/vid-1/multipart/presign-part", bytes.NewReader([]byte(`{"part_number":3}`)))
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if st.presignedNum != 3 || st.presignedKey != "raw/vid-1/original.mp4" {
		t.Fatalf("presign args num=%d key=%s", st.presignedNum, st.presignedKey)
	}
}

func TestMultipartPresignPartInvalidRange(t *testing.T) {
	h := NewMultipartHandler(&fakeMultipartStorage{}, &fakePub{}, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	for _, pn := range []int{0, -1, 10001} {
		req := httptest.NewRequest("POST", "/api/v1/videos/vid-1/multipart/presign-part", bytes.NewReader([]byte(`{"part_number":`+strconv.Itoa(pn)+`}`)))
		req = withAuth(req, "owner1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("part %d: want 400 got %d %s", pn, w.Code, w.Body.String())
		}
	}
}

func TestMultipartCompleteSuccess(t *testing.T) {
	st := &fakeMultipartStorage{
		uploadID: "up-1",
		parts: []storage.MultipartPart{
			{PartNumber: 1, Size: 8 << 20, ETag: "etag1"},
			{PartNumber: 2, Size: 4 << 20, ETag: "etag2"},
		},
		completeSize: 12 << 20,
	}
	pub := &fakePub{}
	h := NewMultipartHandler(st, pub, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/vid-1/multipart/complete", nil)
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if !pub.called {
		t.Fatalf("publisher not called")
	}
	ev, ok := pub.payload.(VideoUploadedEvent)
	if !ok || ev.VideoID != "vid-1" || ev.S3Key != "raw/vid-1/original.mp4" {
		t.Fatalf("bad event %+v", pub.payload)
	}
}

func TestMultipartCompleteNoParts(t *testing.T) {
	st := &fakeMultipartStorage{uploadID: "up-1"}
	pub := &fakePub{}
	h := NewMultipartHandler(st, pub, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/vid-1/multipart/complete", nil)
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("want 400 got %d %s", w.Code, w.Body.String())
	}
	if pub.called {
		t.Fatalf("publisher must not be called")
	}
}

func TestMultipartCompleteNoUpload(t *testing.T) {
	st := &fakeMultipartStorage{findErr: errFake("multipart upload not found for raw/vid-1/original.mp4")}
	h := NewMultipartHandler(st, &fakePub{}, &fakeMeta{id: "vid-1"}, &fakeOwnership{owner: "owner1"})
	r := newMultipartRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/vid-1/multipart/complete", nil)
	req = withAuth(req, "owner1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("want 404 got %d %s", w.Code, w.Body.String())
	}
}
