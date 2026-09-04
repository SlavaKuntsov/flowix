package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeResumableStorage struct {
	data map[string][]byte
}

func newFakeResumable() *fakeResumableStorage {
	return &fakeResumableStorage{data: map[string][]byte{}}
}
func (f *fakeResumableStorage) StatObject(ctx context.Context, key string) error {
	if _, ok := f.data[key]; !ok {
		return errFake("not found")
	}
	return nil
}
func (f *fakeResumableStorage) StatObjectSize(ctx context.Context, key string) (int64, error) {
	if d, ok := f.data[key]; ok {
		return int64(len(d)), nil
	}
	return 0, errFake("not found")
}
func (f *fakeResumableStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if d, ok := f.data[key]; ok {
		return io.NopCloser(bytes.NewReader(d)), nil
	}
	return nil, errFake("not found")
}
func (f *fakeResumableStorage) PutObject(ctx context.Context, key string, r io.Reader, size int64, ct string) error {
	b, _ := io.ReadAll(r)
	f.data[key] = b
	return nil
}


func newResumableRouter(h *ResumableHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/videos/{id}/resumable", h.Status)
	r.Put("/api/v1/videos/{id}/resumable", h.Upload)
	return r
}

func TestResumableStatusEmpty(t *testing.T) {
	st := newFakeResumable()
	h := NewResumableHandler(st)
	r := newResumableRouter(h)
	req := httptest.NewRequest("GET", "/api/v1/videos/vid1/resumable", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"uploaded":0`)) {
		t.Fatalf("want uploaded 0 got %s", w.Body.String())
	}
}

func TestResumableUploadSingleChunk(t *testing.T) {
	st := newFakeResumable()
	h := NewResumableHandler(st)
	r := newResumableRouter(h)
	// first chunk 0-4/10
	body := bytes.Repeat([]byte("a"), 5)
	req := httptest.NewRequest("PUT", "/api/v1/videos/vid1/resumable", bytes.NewReader(body))
	req.Header.Set("Content-Range", "bytes 0-4/10")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 308 {
		t.Fatalf("want 308 got %d %s", w.Code, w.Body.String())
	}
	// second chunk 5-9/10
	body2 := bytes.Repeat([]byte("b"), 5)
	req2 := httptest.NewRequest("PUT", "/api/v1/videos/vid1/resumable", bytes.NewReader(body2))
	req2.Header.Set("Content-Range", "bytes 5-9/10")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("want 200 got %d %s", w2.Code, w2.Body.String())
	}
	key := "raw/vid1/original.mp4"
	if len(st.data[key]) != 10 {
		t.Fatalf("want 10 bytes got %d", len(st.data[key]))
	}
}

func TestResumableRangeMismatch(t *testing.T) {
	st := newFakeResumable()
	st.data["raw/vid1/original.mp4"] = bytes.Repeat([]byte("x"), 5)
	h := NewResumableHandler(st)
	r := newResumableRouter(h)
	body := bytes.Repeat([]byte("a"), 5)
	req := httptest.NewRequest("PUT", "/api/v1/videos/vid1/resumable", bytes.NewReader(body))
	req.Header.Set("Content-Range", "bytes 0-4/10") // should be 5-9
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 416 {
		t.Fatalf("want 416 got %d %s", w.Code, w.Body.String())
	}
}

func TestResumableFullPutNoRange(t *testing.T) {
	st := newFakeResumable()
	h := NewResumableHandler(st)
	r := newResumableRouter(h)
	body := bytes.Repeat([]byte("z"), 7)
	req := httptest.NewRequest("PUT", "/api/v1/videos/vid1/resumable", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if len(st.data["raw/vid1/original.mp4"]) != 7 {
		t.Fatalf("want 7 got %d", len(st.data["raw/vid1/original.mp4"]))
	}
}
