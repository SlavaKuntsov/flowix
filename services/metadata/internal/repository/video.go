// Package repository handles persistence for video metadata.
package repository

import (
	"context"
	"errors"
	"fmt"

	"flowix/metadata/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VideoRepo struct {
	pool *pgxpool.Pool
}

func NewVideoRepo(pool *pgxpool.Pool) *VideoRepo { return &VideoRepo{pool: pool} }

func (r *VideoRepo) Create(ctx context.Context, ownerID, title, description string) (*model.Video, error) {
	id := uuid.New().String()
	vis := model.VisibilityPublic
	q := `INSERT INTO videos (id, owner_id, title, description, status, visibility) VALUES ($1,$2,$3,$4,'uploaded',$5) RETURNING id, owner_id, title, description, duration, status, visibility, thumbnail_s3_key, created_at, updated_at`
	v := &model.Video{}
	err := r.pool.QueryRow(ctx, q, id, ownerID, title, description, vis).Scan(&v.ID, &v.OwnerID, &v.Title, &v.Description, &v.Duration, &v.Status, &v.Visibility, &v.ThumbnailS3Key, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create video: %w", err)
	}
	if v.ThumbnailS3Key != nil {
		u := "/thumbnails/" + v.ID + "/thumb.jpg"
		v.ThumbnailURL = &u
	}
	return v, nil
}

func (r *VideoRepo) GetByID(ctx context.Context, id string) (*model.Video, error) {
	q := `SELECT v.id, v.owner_id, u.email, v.title, v.description, v.duration, v.status, COALESCE(v.visibility::text,'public'), v.thumbnail_s3_key, v.created_at, v.updated_at FROM videos v LEFT JOIN users u ON u.id=v.owner_id WHERE v.id=$1`
	v := &model.Video{}
	err := r.pool.QueryRow(ctx, q, id).Scan(&v.ID, &v.OwnerID, &v.OwnerEmail, &v.Title, &v.Description, &v.Duration, &v.Status, &v.Visibility, &v.ThumbnailS3Key, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if v.ThumbnailS3Key != nil {
		u := "/thumbnails/" + v.ID + "/thumb.jpg"
		v.ThumbnailURL = &u
	}
	// renditions
	rq := `SELECT video_id, quality, bitrate, width, height, s3_key FROM video_renditions WHERE video_id=$1`
	rows, err := r.pool.Query(ctx, rq, id)
	if err != nil {
		return v, nil
	}
	defer rows.Close()
	for rows.Next() {
		var rn model.Rendition
		if err := rows.Scan(&rn.VideoID, &rn.Quality, &rn.Bitrate, &rn.Width, &rn.Height, &rn.S3Key); err == nil {
			v.Renditions = append(v.Renditions, rn)
		}
	}
	return v, nil
}

// List returns videos visible to viewerID: everyone sees public, the owner
// also sees their own private/unlisted (issue #44). Empty viewerID = anonymous.
func (r *VideoRepo) List(ctx context.Context, limit, offset int, viewerID string) ([]model.Video, error) {
	// $3 участвует и в текстовом сравнении ($3 <> ''), и в сравнении с uuid-колонкой:
	// без ::uuid Postgres выводит тип $3 как text и падает с SQLSTATE 42883 (uuid = text)
	q := `SELECT v.id, v.owner_id, u.email, v.title, v.description, v.duration, v.status, COALESCE(v.visibility::text,'public'), v.thumbnail_s3_key, v.created_at, v.updated_at FROM videos v LEFT JOIN users u ON u.id=v.owner_id WHERE v.visibility='public' OR ($3 <> '' AND v.owner_id=$3::uuid) ORDER BY v.created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, limit, offset, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Video
	for rows.Next() {
		var v model.Video
		if err := rows.Scan(&v.ID, &v.OwnerID, &v.OwnerEmail, &v.Title, &v.Description, &v.Duration, &v.Status, &v.Visibility, &v.ThumbnailS3Key, &v.CreatedAt, &v.UpdatedAt); err == nil {
			if v.ThumbnailS3Key != nil {
				u := "/thumbnails/" + v.ID + "/thumb.jpg"
				v.ThumbnailURL = &u
			}
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func (r *VideoRepo) Update(ctx context.Context, id, ownerID string, req model.UpdateVideoRequest) (*model.Video, error) {
	// issue #57: один атомарный UPDATE вместо read-check-write (TOCTOU) и 3 отдельных
	// UPDATE. owner в WHERE закрывает forbidden-проверку, COALESCE не трогает поля,
	// не переданные в запросе — конкурентные PATCH не затирают изменения друг друга.
	// Email берётся подзапросом в RETURNING, чтобы остаться в одном запросе.
	if req.Visibility != nil && !req.Visibility.Valid() {
		return nil, fmt.Errorf("invalid visibility")
	}
	q := `UPDATE videos v SET title=COALESCE($1, v.title), description=COALESCE($2, v.description), visibility=COALESCE($3::video_visibility, v.visibility) WHERE v.id=$4 AND v.owner_id=$5 RETURNING v.id, v.owner_id, (SELECT email FROM users WHERE id=v.owner_id), v.title, v.description, v.duration, v.status, v.visibility::text, v.thumbnail_s3_key, v.created_at, v.updated_at`
	v := &model.Video{}
	err := r.pool.QueryRow(ctx, q, req.Title, req.Description, req.Visibility, id, ownerID).Scan(&v.ID, &v.OwnerID, &v.OwnerEmail, &v.Title, &v.Description, &v.Duration, &v.Status, &v.Visibility, &v.ThumbnailS3Key, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// no-row в WHERE неразличим: чужое видео или не существует — классифицируем
		// отдельным чтением, чтобы сохранить контракт ответов 403/404
		var exists bool
		if e := r.pool.QueryRow(ctx, `SELECT true FROM videos WHERE id=$1`, id).Scan(&exists); e == nil {
			return nil, fmt.Errorf("forbidden")
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if v.ThumbnailS3Key != nil {
		u := "/thumbnails/" + v.ID + "/thumb.jpg"
		v.ThumbnailURL = &u
	}
	return v, nil
}

func (r *VideoRepo) Delete(ctx context.Context, id, ownerID string) error {
	v, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if v.OwnerID != ownerID {
		return fmt.Errorf("forbidden")
	}
	_, err = r.pool.Exec(ctx, `DELETE FROM videos WHERE id=$1`, id)
	return err
}

func (r *VideoRepo) UpdateThumbnail(ctx context.Context, id string, thumbnailS3Key string) error {
	_, err := r.pool.Exec(ctx, `UPDATE videos SET thumbnail_s3_key=$1 WHERE id=$2`, thumbnailS3Key, id)
	return err
}

func (r *VideoRepo) UpdateStatus(ctx context.Context, id string, status model.VideoStatus, renditions []model.Rendition) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cur string
	if err := tx.QueryRow(ctx, `SELECT status FROM videos WHERE id=$1 FOR UPDATE`, id).Scan(&cur); err != nil {
		return err
	}
	// Idempotency: don't downgrade terminal ready → processing/uploaded (redelivery after heartbeat timeout).
	if cur == string(model.StatusReady) && (status == model.StatusProcessing || status == model.StatusUploaded) {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE videos SET status=$1 WHERE id=$2`, status, id); err != nil {
		return err
	}
	if len(renditions) > 0 {
		for _, rn := range renditions {
			_, err := tx.Exec(ctx, `INSERT INTO video_renditions (video_id, quality, bitrate, width, height, s3_key) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (video_id, quality) DO UPDATE SET bitrate=EXCLUDED.bitrate, width=EXCLUDED.width, height=EXCLUDED.height, s3_key=EXCLUDED.s3_key`, id, rn.Quality, rn.Bitrate, rn.Width, rn.Height, rn.S3Key)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
