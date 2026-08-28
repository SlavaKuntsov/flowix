package model

import "time"

type VideoStatus string

const (
	StatusUploaded   VideoStatus = "uploaded"
	StatusProcessing VideoStatus = "processing"
	StatusReady      VideoStatus = "ready"
	StatusFailed     VideoStatus = "failed"
)

type Video struct {
	ID             string      `json:"id"`
	OwnerID        string      `json:"owner_id"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	Duration       *int        `json:"duration,omitempty"`
	Status         VideoStatus `json:"status"`
	ThumbnailS3Key *string     `json:"thumbnail_s3_key,omitempty"`
	ThumbnailURL   *string     `json:"thumbnail_url,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	Renditions     []Rendition `json:"renditions,omitempty"`
}

type Rendition struct {
	VideoID string `json:"video_id"`
	Quality string `json:"quality"`
	Bitrate int    `json:"bitrate"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	S3Key   string `json:"s3_key"`
}

type CreateVideoRequest struct {
	Title       string `json:"title" validate:"required"`
	Description string `json:"description"`
}

type UpdateVideoRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type UpdateStatusRequest struct {
	Status         VideoStatus `json:"status"`
	Renditions     []Rendition `json:"renditions"`
	ThumbnailS3Key *string     `json:"thumbnail_s3_key,omitempty"`
	ThumbnailURL   *string     `json:"thumbnail_url,omitempty"` // alias for thumbnail_s3_key
}
