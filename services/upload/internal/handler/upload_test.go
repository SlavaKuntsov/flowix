package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	mw "flowix/upload/internal/middleware"

	"github.com/golang-jwt/jwt/v5"
)

const secret = "test-secret-32chars-for-upload-tests!!"

func token(sub string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub})
	s, _ := t.SignedString([]byte(secret))
	return s
}

type fakeMeta struct {
	id  string
	err error
}

func (f *fakeMeta) CreateVideo(_, _, _ string) (string, error) { return f.id, f.err }

type fakeStorage struct {
	called bool
	key    string
	err    error
}

func (f *fakeStorage) PutObject(_ context.Context, key string, _ io.Reader, _ int64, _ string) error {
	f.called = true
	f.key = key
	return f.err
}

type fakePub struct {
	called  bool
	payload interface{}
	err     error
}

func (f *fakePub) Publish(_ context.Context, p interface{}) error {
	f.called = true
	f.payload = p
	return f.err
}

func newMultipart(field, filename, content string, extra map[string]string) (*bytes.Buffer, string) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile(field, filename)
	_, _ = fw.Write([]byte(content))
	for k, v := range extra {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	return &b, w.FormDataContentType()
}

func authContext(req *http.Request, userID string) *http.Request {
	ctx := context.WithValue(req.Context(), mw.UserIDKey, userID)
	return req.WithContext(ctx)
}

func TestUploadSuccess(t *testing.T) {
	meta := &fakeMeta{id: "vid-123"}
	st := &fakeStorage{}
	pub := &fakePub{}
	h := NewUploadHandler(st, pub, meta)

	body, ctype := newMultipart("file", "test.mp4", "fakecontent", map[string]string{"title": "hello"})
	req := httptest.NewRequest("POST", "/api/v1/videos/upload", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token("owner1"))
	req = authContext(req, "owner1")

	w := httptest.NewRecorder()
	h.Upload(w, req)
	if w.Code != 201 {
		t.Fatalf("want 201 got %d body %s", w.Code, w.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] != "vid-123" {
		t.Fatalf("want vid-123 got %s", out["id"])
	}
	if out["s3_key"] != "raw/vid-123/original.mp4" {
		t.Fatalf("bad s3_key %s", out["s3_key"])
	}
	if !st.called || st.key != "raw/vid-123/original.mp4" {
		t.Fatalf("storage not called or bad key")
	}
	if !pub.called {
		t.Fatalf("publisher not called")
	}
	ev, ok := pub.payload.(VideoUploadedEvent)
	if !ok {
		t.Fatalf("bad payload type %T", pub.payload)
	}
	if ev.OwnerID != "owner1" || ev.VideoID != "vid-123" {
		t.Fatalf("bad event %+v", ev)
	}
}

func TestUploadMissingFile(t *testing.T) {
	meta := &fakeMeta{id: "vid-123"}
	h := NewUploadHandler(&fakeStorage{}, &fakePub{}, meta)
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("title", "hello")
	_ = w.Close()
	req := httptest.NewRequest("POST", "/api/v1/videos/upload", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token("owner1"))
	req = authContext(req, "owner1")
	rec := httptest.NewRecorder()
	h.Upload(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 got %d %s", rec.Code, rec.Body.String())
	}
}

func TestUploadMetadataError(t *testing.T) {
	meta := &fakeMeta{err: errFake("down")}
	st := &fakeStorage{}
	pub := &fakePub{}
	h := NewUploadHandler(st, pub, meta)
	body, ctype := newMultipart("file", "test.mp4", "c", nil)
	req := httptest.NewRequest("POST", "/api/v1/videos/upload", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token("owner1"))
	req = authContext(req, "owner1")
	w := httptest.NewRecorder()
	h.Upload(w, req)
	if w.Code != 502 {
		t.Fatalf("want 502 got %d %s", w.Code, w.Body.String())
	}
	if st.called || pub.called {
		t.Fatalf("should not call storage/publisher on meta error")
	}
}

func TestUploadStorageError(t *testing.T) {
	meta := &fakeMeta{id: "vid-1"}
	st := &fakeStorage{err: errFake("disk")}
	pub := &fakePub{}
	h := NewUploadHandler(st, pub, meta)
	body, ctype := newMultipart("file", "test.mp4", "c", nil)
	req := httptest.NewRequest("POST", "/api/v1/videos/upload", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token("owner1"))
	req = authContext(req, "owner1")
	w := httptest.NewRecorder()
	h.Upload(w, req)
	if w.Code != 500 {
		t.Fatalf("want 500 got %d %s", w.Code, w.Body.String())
	}
	if pub.called {
		t.Fatalf("should not publish on storage error")
	}
}

func TestUploadPublishError(t *testing.T) {
	meta := &fakeMeta{id: "vid-1"}
	st := &fakeStorage{}
	pub := &fakePub{err: errFake("queue")}
	h := NewUploadHandler(st, pub, meta)
	body, ctype := newMultipart("file", "test.mp4", "c", nil)
	req := httptest.NewRequest("POST", "/api/v1/videos/upload", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token("owner1"))
	req = authContext(req, "owner1")
	w := httptest.NewRecorder()
	h.Upload(w, req)
	if w.Code != 500 {
		t.Fatalf("want 500 got %d %s", w.Code, w.Body.String())
	}
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }
func errFake(s string) error    { return fakeErr(s) }
