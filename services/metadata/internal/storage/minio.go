package storage

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog/log"
)

type MinioClient struct {
	client *minio.Client
	bucket string
}

func NewMinioClient(endpoint, accessKey, secretKey, bucket string, secure bool) (*MinioClient, error) {
	cl, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("minio new: %w", err)
	}
	return &MinioClient{client: cl, bucket: bucket}, nil
}

// RemoveObjects deletes keys (best-effort, logs warnings). Missing keys are ignored.
func (m *MinioClient) RemoveObjects(ctx context.Context, keys []string) {
	for _, k := range keys {
		if k == "" {
			continue
		}
		if err := m.client.RemoveObject(ctx, m.bucket, k, minio.RemoveObjectOptions{}); err != nil {
			log.Warn().Err(err).Str("key", k).Msg("minio remove failed")
		} else {
			log.Info().Str("key", k).Msg("minio removed")
		}
	}
}

// RemovePrefix deletes all objects with given prefix (for cleaning orphans when video was deleted mid-transcode).
func (m *MinioClient) RemovePrefix(ctx context.Context, prefix string) {
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			log.Warn().Err(obj.Err).Str("prefix", prefix).Msg("list for remove failed")
			continue
		}
		if err := m.client.RemoveObject(ctx, m.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			log.Warn().Err(err).Str("key", obj.Key).Msg("minio remove prefix failed")
		} else {
			log.Info().Str("key", obj.Key).Msg("minio removed prefix")
		}
	}
}
