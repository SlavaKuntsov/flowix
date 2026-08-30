package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"flowix/metadata/internal/middleware"
	"flowix/metadata/internal/model"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-32chars-for-unit-tests-!!"

func signToken(sub string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub})
	s, _ := t.SignedString([]byte(testSecret))
	return s
}

// fakeStore in-memory impl of VideoStore — T2 stepwise without Postgres.
type fakeStore struct {
	videos map[string]*model.Video
}

func newFake() *fakeStore { return &fakeStore{videos: map[string]*model.Video{}} }

func (f *fakeStore) Create(_ context.Context, ownerID, title, description string) (*model.Video, error) {
	v := &model.Video{ID: "vid-" + title, OwnerID: ownerID, Title: title, Description: description, Status: model.StatusUploaded}
	f.videos[v.ID] = v
	return v, nil
}
func (f *fakeStore) GetByID(_ context.Context, id string) (*model.Video, error) {
	v, ok := f.videos[id]
	if !ok {
		return nil, errNotFound()
	}
	return v, nil
}
func (f *fakeStore) List(_ context.Context, limit, offset int) ([]model.Video, error) {
	out := []model.Video{}
	for _, v := range f.videos {
		out = append(out, *v)
	}
	if offset < len(out) {
		out = out[offset:]
	} else {
		out = nil
	}
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeStore) Update(_ context.Context, id, ownerID string, req model.UpdateVideoRequest) (*model.Video, error) {
	v, ok := f.videos[id]
	if !ok {
		return nil, errNotFound()
	}
	if v.OwnerID != ownerID {
		return nil, errForbidden()
	}
	if req.Title != nil {
		v.Title = *req.Title
	}
	if req.Description != nil {
		v.Description = *req.Description
	}
	return v, nil
}
func (f *fakeStore) Delete(_ context.Context, id, ownerID string) error {
	v, ok := f.videos[id]
	if !ok {
		return errNotFound()
	}
	if v.OwnerID != ownerID {
		return errForbidden()
	}
	delete(f.videos, id)
	return nil
}
func (f *fakeStore) UpdateStatus(_ context.Context, id string, status model.VideoStatus, renditions []model.Rendition) error {
	v, ok := f.videos[id]
	if !ok {
		return errNotFound()
	}
	// idempotency: don't downgrade ready → processing/uploaded
	if v.Status == model.StatusReady && (status == model.StatusProcessing || status == model.StatusUploaded) {
		return nil
	}
	v.Status = status
	if len(renditions) > 0 {
		v.Renditions = renditions
	}
	return nil
}
func (f *fakeStore) UpdateThumbnail(_ context.Context, id string, thumbnailS3Key string) error {
	v, ok := f.videos[id]
	if !ok {
		return errNotFound()
	}
	v.ThumbnailS3Key = &thumbnailS3Key
	u := "/thumbnails/" + id + "/thumb.jpg"
	v.ThumbnailURL = &u
	return nil
}

func errNotFound() error  { return &fakeErr{"not found"} }
func errForbidden() error { return &fakeErr{"forbidden"} }

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }

// helper to build router with auth where needed
func testRouter(store VideoStore) chi.Router {
	r := chi.NewRouter()
	vh := NewVideoHandler(store)
	// protected
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(testSecret))
		r.Post("/api/v1/videos", vh.Create)
		r.Patch("/api/v1/videos/{id}", vh.Update)
		r.Delete("/api/v1/videos/{id}", vh.Delete)
	})
	r.Get("/api/v1/videos", vh.List)
	r.Get("/api/v1/videos/{id}", vh.Get)
	r.Patch("/internal/videos/{id}/status", vh.UpdateStatus)
	r.Get("/internal/videos/{id}", vh.GetInternal)
	r.Get("/internal/videos/{id}/vod", vh.GetVODMapping)
	return r
}

func TestCreateSuccess(t *testing.T) {
	r := testRouter(newFake())
	body, _ := json.Marshal(map[string]string{"title": "hello", "description": "d"})
	req := httptest.NewRequest("POST", "/api/v1/videos", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signToken("owner1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("want 201 got %d body %s", w.Code, w.Body.String())
	}
	var v model.Video
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Title != "hello" || v.OwnerID != "owner1" {
		t.Fatalf("unexpected video %+v", v)
	}
}

func TestCreateMissingTitle(t *testing.T) {
	r := testRouter(newFake())
	body, _ := json.Marshal(map[string]string{"title": ""})
	req := httptest.NewRequest("POST", "/api/v1/videos", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signToken("owner1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestCreateNoAuth(t *testing.T) {
	r := testRouter(newFake())
	body, _ := json.Marshal(map[string]string{"title": "x"})
	req := httptest.NewRequest("POST", "/api/v1/videos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestGetAndList(t *testing.T) {
	store := newFake()
	store.videos["vid-1"] = &model.Video{ID: "vid-1", OwnerID: "o1", Title: "t1", Status: model.StatusUploaded}
	store.videos["vid-2"] = &model.Video{ID: "vid-2", OwnerID: "o1", Title: "t2", Status: model.StatusReady}
	r := testRouter(store)

	req := httptest.NewRequest("GET", "/api/v1/videos/vid-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get want 200 got %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/v1/videos?limit=1&offset=0", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list want 200 got %d", w.Code)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["limit"].(float64) != 1 {
		t.Fatalf("limit want 1 got %v", out["limit"])
	}

	req = httptest.NewRequest("GET", "/api/v1/videos/notfound", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("want 404 got %d", w.Code)
	}
}

func TestUpdateForbidden(t *testing.T) {
	store := newFake()
	store.videos["vid-1"] = &model.Video{ID: "vid-1", OwnerID: "owner1", Title: "t", Status: model.StatusUploaded}
	r := testRouter(store)
	title := "new"
	body, _ := json.Marshal(map[string]string{"title": title})
	req := httptest.NewRequest("PATCH", "/api/v1/videos/vid-1", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signToken("owner2"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403 got %d %s", w.Code, w.Body.String())
	}

	// owner success
	req = httptest.NewRequest("PATCH", "/api/v1/videos/vid-1", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signToken("owner1"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestDelete(t *testing.T) {
	store := newFake()
	store.videos["vid-1"] = &model.Video{ID: "vid-1", OwnerID: "owner1", Title: "t", Status: model.StatusUploaded}
	r := testRouter(store)
	req := httptest.NewRequest("DELETE", "/api/v1/videos/vid-1", nil)
	req.Header.Set("Authorization", "Bearer "+signToken("owner2"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403 got %d", w.Code)
	}
	req = httptest.NewRequest("DELETE", "/api/v1/videos/vid-1", nil)
	req.Header.Set("Authorization", "Bearer "+signToken("owner1"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("want 204 got %d %s", w.Code, w.Body.String())
	}
}

func TestUpdateStatusInternal(t *testing.T) {
	store := newFake()
	store.videos["vid-1"] = &model.Video{ID: "vid-1", OwnerID: "owner1", Title: "t", Status: model.StatusUploaded}
	r := testRouter(store)

	// valid
	for _, st := range []string{"processing", "ready", "failed"} {
		body, _ := json.Marshal(map[string]string{"status": st})
		req := httptest.NewRequest("PATCH", "/internal/videos/vid-1/status", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status %s want 200 got %d %s", st, w.Code, w.Body.String())
		}
	}

	// invalid status
	body, _ := json.Marshal(map[string]string{"status": "bad"})
	req := httptest.NewRequest("PATCH", "/internal/videos/vid-1/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("want 400 got %d", w.Code)
	}

	// with renditions
	body, _ = json.Marshal(model.UpdateStatusRequest{Status: model.StatusReady, Renditions: []model.Rendition{{VideoID: "vid-1", Quality: "720p", Bitrate: 2500, Width: 1280, Height: 720, S3Key: "renditions/vid-1/720p.mp4"}}})
	req = httptest.NewRequest("PATCH", "/internal/videos/vid-1/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if len(store.videos["vid-1"].Renditions) != 1 {
		t.Fatalf("want renditions 1 got %d", len(store.videos["vid-1"].Renditions))
	}
}

func TestGetVODMapping(t *testing.T) {
	store := newFake()
	store.videos["vid-1"] = &model.Video{
		ID: "vid-1", Status: model.StatusReady,
		Renditions: []model.Rendition{
			{Quality: "1080p", Bitrate: 5000, S3Key: "renditions/vid-1/1080p.mp4"},
			{Quality: "360p", Bitrate: 800, S3Key: "renditions/vid-1/360p.mp4"},
			{Quality: "720p", Bitrate: 2500, S3Key: "renditions/vid-1/720p.mp4"},
		},
	}
	r := testRouter(store)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/internal/videos/vid-1/vod", nil))
	if w.Code != 200 {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var got vodMapping
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sequences) != 3 || got.Sequences[0].Clips[0].Path != "/renditions/vid-1/360p.mp4" {
		t.Fatalf("unexpected mapping: %+v", got)
	}
}

func TestUpdateStatusIdempotencyReadyNotDowngraded(t *testing.T) {
	store := newFake()
	store.videos["vid-1"] = &model.Video{ID: "vid-1", OwnerID: "owner1", Title: "t", Status: model.StatusReady}
	r := testRouter(store)
	// try to downgrade ready -> processing should be ignored (idempotent)
	body, _ := json.Marshal(map[string]string{"status": "processing"})
	req := httptest.NewRequest("PATCH", "/internal/videos/vid-1/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	if store.videos["vid-1"].Status != model.StatusReady {
		t.Fatalf("ready should not be downgraded to processing, got %s", store.videos["vid-1"].Status)
	}
	// ready -> failed should be allowed (terminal)
	body, _ = json.Marshal(map[string]string{"status": "failed"})
	req = httptest.NewRequest("PATCH", "/internal/videos/vid-1/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	if store.videos["vid-1"].Status != model.StatusFailed {
		t.Fatalf("ready -> failed should be allowed, got %s", store.videos["vid-1"].Status)
	}
}
