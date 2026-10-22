// Package reconciler self-heals videos stuck in status 'uploaded'.
//
// Every upload path creates the metadata row before the bytes land, and the
// only thing that moves a row out of 'uploaded' is the transcoder consuming the
// video.uploaded event. So an upload that never completes (closed tab, lost
// resume key, network drop, publish failure, a queue that was recreated) leaves
// a row that hangs forever — the UI then shows an endless "Transcoding…".
//
// The reconciler periodically looks at 'uploaded' rows older than ttl and:
//   - re-publishes video.uploaded when the raw object exists (a lost event), or
//   - aborts any dangling multipart session and marks the row failed when it
//     does not (an abandoned upload).
package reconciler

import (
	"context"
	"time"

	"flowix/upload/internal/client"
	"flowix/upload/internal/handler"
	"github.com/rs/zerolog/log"
)

// Metadata lists stale uploads and can fail them.
type Metadata interface {
	ListStaleUploaded(olderThan time.Duration, limit int) ([]client.StaleUpload, error)
	SetStatus(videoID, status string) error
}

// Storage inspects and cleans the raw object / multipart session.
type Storage interface {
	StatObjectSize(ctx context.Context, key string) (int64, error)
	FindUploadID(ctx context.Context, key string) (string, error)
	AbortMultipartUpload(ctx context.Context, key, uploadID string) error
}

// Publisher re-emits video.uploaded (implemented by queue.Publisher).
type Publisher interface {
	Publish(ctx context.Context, payload interface{}) error
}

// Reconciler periodically reconciles stale 'uploaded' rows.
type Reconciler struct {
	metadata  Metadata
	storage   Storage
	publisher Publisher
	ttl       time.Duration
	limit     int
}

func New(m Metadata, s Storage, p Publisher, ttl time.Duration, limit int) *Reconciler {
	if limit <= 0 {
		limit = 100
	}
	return &Reconciler{metadata: m, storage: s, publisher: p, ttl: ttl, limit: limit}
}

// Run reconciles every interval until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.reconcile(ctx)
		}
	}
}

func rawKey(videoID string) string { return "raw/" + videoID + "/original.mp4" }

// reconcile is exported for tests via reconcileOnce.
func (r *Reconciler) reconcile(ctx context.Context) {
	stale, err := r.metadata.ListStaleUploaded(r.ttl, r.limit)
	if err != nil {
		log.Error().Err(err).Msg("reconciler: list stale uploads failed")
		return
	}
	for _, v := range stale {
		r.handle(ctx, v)
	}
}

func (r *Reconciler) handle(ctx context.Context, v client.StaleUpload) {
	key := rawKey(v.ID)
	// Object present → the upload finished but the event never reached the
	// queue (publish failure / queue recreated). Re-publish; the transcoder
	// skips videos already ready/failed.
	if size, err := r.storage.StatObjectSize(ctx, key); err == nil && size > 0 {
		ev := handler.VideoUploadedEvent{VideoID: v.ID, S3Key: key, OwnerID: v.OwnerID}
		if err := r.publisher.Publish(ctx, ev); err != nil {
			log.Error().Err(err).Str("video_id", v.ID).Msg("reconciler: republish failed")
			return
		}
		log.Info().Str("video_id", v.ID).Msg("reconciler: re-published lost video.uploaded")
		return
	}
	// No object → the upload never completed. Drop any dangling multipart
	// session so its parts don't linger, then fail the row.
	if uploadID, err := r.storage.FindUploadID(ctx, key); err == nil {
		if err := r.storage.AbortMultipartUpload(ctx, key, uploadID); err != nil {
			log.Warn().Err(err).Str("video_id", v.ID).Msg("reconciler: abort multipart failed")
		}
	}
	if err := r.metadata.SetStatus(v.ID, "failed"); err != nil {
		log.Error().Err(err).Str("video_id", v.ID).Msg("reconciler: mark failed failed")
		return
	}
	log.Info().Str("video_id", v.ID).Msg("reconciler: marked abandoned upload failed")
}

// ReconcileOnce runs a single pass (used by tests).
func (r *Reconciler) ReconcileOnce(ctx context.Context) { r.reconcile(ctx) }
