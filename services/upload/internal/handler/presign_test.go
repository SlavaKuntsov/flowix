package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mw "flowix/upload/internal/middleware"
	"github.com/go-chi/chi/v5"
)

type fakePresignStorage struct {
	url string
	err error
	statErr error
	presignedKey string
}

func (f *fakePresignStorage) PresignedPutObjectExternal(_ context.Context, key string, _ time.Duration, _ string) (string, error) {
	f.presignedKey = key
	if f.err != nil {
		return "", f.err
	}
	if f.url != "" {
		return f.url, nil
	}
	return "http://localhost:9000/videos/" + key + "?X-Amz-Signature=presigned", nil
}
func (f *fakePresignStorage) StatObject(_ context.Context, key string) error {
	if f.statErr != nil {
		return f.statErr
	}
	return nil
}

func TestPresignSuccess(t *testing.T) {
	meta := &fakeMeta{id: "vid-123"}
	st := &fakePresignStorage{}
	pub := &fakePub{}
	h := NewPresignHandler(st, pub, meta)

	body, _ := json.Marshal(map[string]string{"title": "hello", "filename": "test.mp4", "content_type": "video/mp4"})
	req := httptest.NewRequest("POST", "/api/v1/videos/presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token("owner1"))
	req = req.WithContext(context.WithValue(req.Context(), mw.UserIDKey, "owner1"))

	w := httptest.NewRecorder()
	h.Presign(w, req)
	if w.Code != 201 {
		t.Fatalf("want 201 got %d body %s", w.Code, w.Body.String())
	}
	var out presignResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.VideoID != "vid-123" || out.S3Key != "raw/vid-123/original.mp4" {
		t.Fatalf("bad response %+v", out)
	}
	if out.URL == "" || out.Method != "PUT" {
		t.Fatalf("bad url/method %+v", out)
	}
	if st.presignedKey != "raw/vid-123/original.mp4" {
		t.Fatalf("presign key %s", st.presignedKey)
	}
}

func TestPresignInvalidContentType(t *testing.T) {
	meta := &fakeMeta{id: "vid-1"}
	h := NewPresignHandler(&fakePresignStorage{}, &fakePub{}, meta)
	body, _ := json.Marshal(map[string]string{"title": "x", "content_type": "image/jpeg"})
	req := httptest.NewRequest("POST", "/api/v1/videos/presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), mw.UserIDKey, "owner1"))
	w := httptest.NewRecorder()
	h.Presign(w, req)
	if w.Code != 400 {
		t.Fatalf("want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestCompleteSuccess(t *testing.T) {
	meta := &fakeMeta{id: "vid-123"}
	st := &fakePresignStorage{}
	pub := &fakePub{}
	h := NewPresignHandler(st, pub, meta)

	r := newChiRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/vid-123/complete", nil)
	req = req.WithContext(context.WithValue(req.Context(), mw.UserIDKey, "owner1"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if !pub.called {
		t.Fatalf("publisher not called")
	}
	ev, ok := pub.payload.(VideoUploadedEvent)
	if !ok || ev.VideoID != "vid-123" || ev.S3Key != "raw/vid-123/original.mp4" {
		t.Fatalf("bad event %+v", pub.payload)
	}
}

func TestCompleteNotFound(t *testing.T) {
	meta := &fakeMeta{id: "vid-1"}
	st := &fakePresignStorage{statErr: errFake("not found")}
	h := NewPresignHandler(st, &fakePub{}, meta)
	r := newChiRouter(h)
	req := httptest.NewRequest("POST", "/api/v1/videos/vid-1/complete", nil)
	req = req.WithContext(context.WithValue(req.Context(), mw.UserIDKey, "owner1"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("want 404 got %d %s", w.Code, w.Body.String())
	}
}

func newChiRouter(h *PresignHandler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/videos/{id}/complete", h.Complete)
	r.Post("/api/v1/videos/complete", h.Complete)
	return r
}
