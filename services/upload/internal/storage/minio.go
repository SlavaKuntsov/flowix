package storage

import (
	"context"
	"fmt"
	"io"

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
	// ensure bucket exists
	exists, err := cl.BucketExists(context.Background(), bucket)
	if err != nil {
		log.Warn().Err(err).Str("bucket", bucket).Msg("bucket check failed")
	} else if !exists {
		if err := cl.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{}); err != nil {
			log.Warn().Err(err).Str("bucket", bucket).Msg("make bucket failed")
		} else {
			log.Info().Str("bucket", bucket).Msg("bucket created")
		}
	}
	return &MinioClient{client: cl, bucket: bucket}, nil
}

func (m *MinioClient) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}
