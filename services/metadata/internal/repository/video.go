package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"flowix/metadata/internal/model"
)

type VideoRepo struct {
	pool *pgxpool.Pool
}

func NewVideoRepo(pool *pgxpool.Pool) *VideoRepo { return &VideoRepo{pool: pool} }

func (r *VideoRepo) Create(ctx context.Context, ownerID, title, description string) (*model.Video, error) {
	id := uuid.New().String()
	q := `INSERT INTO videos (id, owner_id, title, description, status) VALUES ($1,$2,$3,$4,'uploaded') RETURNING id, owner_id, title, description, duration, status, created_at`
	v := &model.Video{}
	err := r.pool.QueryRow(ctx, q, id, ownerID, title, description).Scan(&v.ID, &v.OwnerID, &v.Title, &v.Description, &v.Duration, &v.Status, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create video: %w", err)
	}
	return v, nil
}

func (r *VideoRepo) GetByID(ctx context.Context, id string) (*model.Video, error) {
	q := `SELECT id, owner_id, title, description, duration, status, created_at FROM videos WHERE id=$1`
	v := &model.Video{}
	err := r.pool.QueryRow(ctx, q, id).Scan(&v.ID, &v.OwnerID, &v.Title, &v.Description, &v.Duration, &v.Status, &v.CreatedAt)
	if err != nil {
		return nil, err
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

func (r *VideoRepo) List(ctx context.Context, limit, offset int) ([]model.Video, error) {
	q := `SELECT id, owner_id, title, description, duration, status, created_at FROM videos ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Video
	for rows.Next() {
		var v model.Video
		if err := rows.Scan(&v.ID, &v.OwnerID, &v.Title, &v.Description, &v.Duration, &v.Status, &v.CreatedAt); err == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func (r *VideoRepo) Update(ctx context.Context, id, ownerID string, req model.UpdateVideoRequest) (*model.Video, error) {
	// only owner can update
	v, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.OwnerID != ownerID {
		return nil, fmt.Errorf("forbidden")
	}
	if req.Title != nil {
		_, err = r.pool.Exec(ctx, `UPDATE videos SET title=$1 WHERE id=$2`, *req.Title, id)
		if err != nil {
			return nil, err
		}
	}
	if req.Description != nil {
		_, err = r.pool.Exec(ctx, `UPDATE videos SET description=$1 WHERE id=$2`, *req.Description, id)
		if err != nil {
			return nil, err
		}
	}
	return r.GetByID(ctx, id)
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

func (r *VideoRepo) UpdateStatus(ctx context.Context, id string, status model.VideoStatus, renditions []model.Rendition) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
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
