// Package storage wraps MinIO access for metadata (delete, presign init).
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog/log"
)

type MinioClient struct {
	client *minio.Client
	// presign is bound to a browser-reachable endpoint (e.g. http://localhost:9000);
	// SigV4 embeds the host, so URLs handed to browsers must be signed against it.
	presign *minio.Client
	bucket  string
}

func NewMinioClient(endpoint, accessKey, secretKey, bucket string, secure bool) (*MinioClient, error) {
	cl, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""),
		// Region must be set — without it PresignedGetObject calls getBucketLocation
		// over the network (fails against loopback-published MinIO in browsers' envs).
		Region: "us-east-1",
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("minio new: %w", err)
	}
	return &MinioClient{client: cl, bucket: bucket}, nil
}

// EnablePublicPresign binds presigned URL generation to a browser-reachable
// endpoint (issue #43: fully private bucket — browsers need signed reads).
func (m *MinioClient) EnablePublicPresign(publicEndpoint, accessKey, secretKey string, secure bool) error {
	cl, err := minio.New(publicEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Region: "us-east-1",
		Secure: secure,
	})
	if err != nil {
		return fmt.Errorf("minio public presign client: %w", err)
	}
	m.presign = cl
	return nil
}

// PresignGetInternal returns a presigned GET URL reachable from inside the
// compose network (nginx-vod upstream fetches).
func (m *MinioClient) PresignGetInternal(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := m.client.PresignedGetObject(ctx, m.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get %s: %w", key, err)
	}
	return u.String(), nil
}

// PresignGetPublic returns a presigned GET URL reachable from user browsers;
// falls back to the internal endpoint when no public endpoint is configured.
func (m *MinioClient) PresignGetPublic(ctx context.Context, key string, expiry time.Duration) (string, error) {
	cl := m.presign
	if cl == nil {
		cl = m.client
	}
	u, err := cl.PresignedGetObject(ctx, m.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get %s: %w", key, err)
	}
	return u.String(), nil
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
