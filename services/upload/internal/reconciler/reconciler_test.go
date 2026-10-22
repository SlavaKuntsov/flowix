package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"flowix/upload/internal/client"
)

type fakeMeta struct {
	stale    []client.StaleUpload
	statuses map[string]string
}

func (f *fakeMeta) ListStaleUploaded(time.Duration, int) ([]client.StaleUpload, error) {
	return f.stale, nil
}
func (f *fakeMeta) SetStatus(id, status string) error {
	f.statuses[id] = status
	return nil
}

type fakeStore struct {
	size    int64
	statErr error
	upload  string
	aborted []string
}

func (f *fakeStore) StatObjectSize(context.Context, string) (int64, error) {
	return f.size, f.statErr
}
func (f *fakeStore) FindUploadID(context.Context, string) (string, error) {
	if f.upload == "" {
		return "", errors.New("multipart upload not found")
	}
	return f.upload, nil
}
func (f *fakeStore) AbortMultipartUpload(_ context.Context, key, id string) error {
	f.aborted = append(f.aborted, key+"|"+id)
	return nil
}

type fakePub struct{ published []interface{} }

func (f *fakePub) Publish(_ context.Context, payload interface{}) error {
	f.published = append(f.published, payload)
	return nil
}

func newRec(m *fakeMeta, s *fakeStore, p *fakePub) *Reconciler {
	return New(m, s, p, time.Hour, 100)
}

// Object present → the upload finished but the event was lost: re-publish.
func TestReconcileRepublishesWhenObjectExists(t *testing.T) {
	m := &fakeMeta{stale: []client.StaleUpload{{ID: "v1", OwnerID: "o1"}}, statuses: map[string]string{}}
	s := &fakeStore{size: 1024}
	p := &fakePub{}
	newRec(m, s, p).ReconcileOnce(context.Background())

	if len(p.published) != 1 {
		t.Fatalf("want 1 republished event, got %d", len(p.published))
	}
	if _, ok := m.statuses["v1"]; ok {
		t.Fatalf("must not fail a completed upload, got %v", m.statuses)
	}
	if len(s.aborted) != 0 {
		t.Fatalf("must not abort a completed upload")
	}
}

// No object, dangling multipart session → abort it and fail the row.
func TestReconcileAbortsSessionAndFails(t *testing.T) {
	m := &fakeMeta{stale: []client.StaleUpload{{ID: "v2"}}, statuses: map[string]string{}}
	s := &fakeStore{statErr: errors.New("not found"), upload: "up-1"}
	p := &fakePub{}
	newRec(m, s, p).ReconcileOnce(context.Background())

	if m.statuses["v2"] != "failed" {
		t.Fatalf("want failed, got %v", m.statuses)
	}
	if len(s.aborted) != 1 || s.aborted[0] != "raw/v2/original.mp4|up-1" {
		t.Fatalf("want multipart abort, got %v", s.aborted)
	}
	if len(p.published) != 0 {
		t.Fatalf("must not republish an upload with no object")
	}
}

// No object, no session → plain abandoned upload: mark failed.
func TestReconcileFailsAbandonedUpload(t *testing.T) {
	m := &fakeMeta{stale: []client.StaleUpload{{ID: "v3"}}, statuses: map[string]string{}}
	s := &fakeStore{statErr: errors.New("not found")}
	p := &fakePub{}
	newRec(m, s, p).ReconcileOnce(context.Background())

	if m.statuses["v3"] != "failed" {
		t.Fatalf("want failed, got %v", m.statuses)
	}
	if len(s.aborted) != 0 {
		t.Fatalf("no session → no abort, got %v", s.aborted)
	}
}
