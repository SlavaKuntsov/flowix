package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog/log"
)

type MinioClient struct {
	client    *minio.Client
	bucket    string
	endpoint  string
	secure    bool
	accessKey string
	secretKey string
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
	return &MinioClient{client: cl, bucket: bucket, endpoint: endpoint, secure: secure, accessKey: accessKey, secretKey: secretKey}, nil
}

func (m *MinioClient) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

func (m *MinioClient) PresignedPutObject(ctx context.Context, key string, expires time.Duration) (string, error) {
	u, err := m.client.PresignedPutObject(ctx, m.bucket, key, expires)
	if err != nil {
		return "", fmt.Errorf("presign put %s: %w", key, err)
	}
	return u.String(), nil
}

// PresignedPutObjectWithURL returns presigned URL for external access.
// Previously we rewrote host after signing, which broke AWS SigV4 (host is signed).
// Now we generate the signature with the public host directly if provided.
func (m *MinioClient) PresignedPutObjectExternal(ctx context.Context, key string, expires time.Duration, publicEndpoint string) (string, error) {
	if publicEndpoint == "" {
		return m.PresignedPutObject(ctx, key, expires)
	}
	pub, err := url.Parse(publicEndpoint)
	if err != nil || pub.Host == "" {
		return m.PresignedPutObject(ctx, key, expires)
	}
	// Build a temporary client that signs for the public host.
	// publicEndpoint may be http://localhost:9000 or https://...
	pubSecure := pub.Scheme == "https"
	// minio.New expects endpoint as host:port without scheme
	pubEndpoint := pub.Host
	// If pubEndpoint lacks port, minio.New will default to 443/80; keep as is.
	tmpClient, err := minio.New(pubEndpoint, &minio.Options{
		Creds:       credentials.NewStaticV4(m.accessKey, m.secretKey, ""),
		Secure:      pubSecure,
		Region:      "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		// fallback to rewriting (should not happen)
		uStr, err2 := m.PresignedPutObject(ctx, key, expires)
		if err2 != nil {
			return "", err2
		}
		if u, e := url.Parse(uStr); e == nil {
			u.Scheme = pub.Scheme
			u.Host = pub.Host
			return u.String(), nil
		}
		return uStr, nil
	}
	u, err := tmpClient.PresignedPutObject(ctx, m.bucket, key, expires)
	if err != nil {
		return "", fmt.Errorf("presign put %s: %w", key, err)
	}
	// Ensure scheme matches publicEndpoint (minio-go uses http/https based on Secure)
	// tmpClient already uses pubSecure, so scheme is correct.
	return u.String(), nil
}

func (m *MinioClient) StatObject(ctx context.Context, key string) error {
	_, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return fmt.Errorf("stat %s: %w", key, err)
	}
	return nil
}

func (m *MinioClient) StatObjectSize(ctx context.Context, key string) (int64, error) {
	info, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", key, err)
	}
	return info.Size, nil
}

func (m *MinioClient) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	return obj, nil
}

func (m *MinioClient) RemoveObject(ctx context.Context, key string) error {
	err := m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("remove %s: %w", key, err)
	}
	return nil
}
