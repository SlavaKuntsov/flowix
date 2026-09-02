package model

import "time"

type VideoStatus string

const (
	StatusUploaded   VideoStatus = "uploaded"
	StatusProcessing VideoStatus = "processing"
	StatusReady      VideoStatus = "ready"
	StatusFailed     VideoStatus = "failed"
)

type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityPrivate  Visibility = "private"
	VisibilityUnlisted Visibility = "unlisted"
)

func (v Visibility) Valid() bool {
	return v == VisibilityPublic || v == VisibilityPrivate || v == VisibilityUnlisted
}

type Video struct {
	ID             string      `json:"id"`
	OwnerID        string      `json:"owner_id"`
	OwnerEmail     *string     `json:"owner_email,omitempty"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	Duration       *int        `json:"duration,omitempty"`
	Status         VideoStatus `json:"status"`
	Visibility     Visibility  `json:"visibility"`
	ThumbnailS3Key *string     `json:"thumbnail_s3_key,omitempty"`
	ThumbnailURL   *string     `json:"thumbnail_url,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
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
	Title       string      `json:"title" validate:"required"`
	Description string      `json:"description"`
	Visibility  *Visibility `json:"visibility,omitempty"`
}

type UpdateVideoRequest struct {
	Title       *string     `json:"title"`
	Description *string     `json:"description"`
	Visibility  *Visibility `json:"visibility,omitempty"`
}

type UpdateStatusRequest struct {
	Status         VideoStatus `json:"status"`
	Renditions     []Rendition `json:"renditions"`
	ThumbnailS3Key *string     `json:"thumbnail_s3_key,omitempty"`
	ThumbnailURL   *string     `json:"thumbnail_url,omitempty"` // alias for thumbnail_s3_key
}
