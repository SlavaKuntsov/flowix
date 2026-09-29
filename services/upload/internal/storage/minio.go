// Package storage implements object storage backends for upload/metadata (MinIO).
package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// PresignedPutObjectExternal returns a presigned PUT URL for external
// (browser) access. Previously we rewrote the host after signing, which broke
// AWS SigV4 (host is signed); now we sign with the public host directly.
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
		Creds:        credentials.NewStaticV4(m.accessKey, m.secretKey, ""),
		Secure:       pubSecure,
		Region:       "us-east-1",
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

// StatObjectInfo returns size and content-type of the object (complete must
// verify the uploaded object matches the presigned upload — issue #46).
func (m *MinioClient) StatObjectInfo(ctx context.Context, key string) (int64, string, error) {
	info, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, "", fmt.Errorf("stat %s: %w", key, err)
	}
	return info.Size, info.ContentType, nil
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

// MultipartPart is a storage-agnostic view of an uploaded S3 part (issue #56).
type MultipartPart struct {
	PartNumber int
	Size       int64
	ETag       string
}

// publicClient returns a client that signs for the browser-visible host.
// SigV4 signs the host, so presigned URLs must be issued for the public
// endpoint (see PresignedPutObjectExternal).
func (m *MinioClient) publicClient(publicEndpoint string) (*minio.Client, error) {
	if publicEndpoint == "" {
		return m.client, nil
	}
	pub, err := url.Parse(publicEndpoint)
	if err != nil || pub.Host == "" {
		return m.client, nil
	}
	tmpClient, err := minio.New(pub.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(m.accessKey, m.secretKey, ""),
		Secure:       pub.Scheme == "https",
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("public client %s: %w", publicEndpoint, err)
	}
	return tmpClient, nil
}

// CreateMultipartUpload starts an S3 multipart session for key (issue #56).
func (m *MinioClient) CreateMultipartUpload(ctx context.Context, key, contentType string) (string, error) {
	if contentType == "" {
		contentType = "video/mp4"
	}
	core := minio.Core{Client: m.client}
	uploadID, err := core.NewMultipartUpload(ctx, m.bucket, key, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("create multipart upload %s: %w", key, err)
	}
	return uploadID, nil
}

// FindUploadID returns the latest in-progress multipart upload for key, or an
// error mentioning "not found" when none exists — the client never tracks
// upload_id, resume works from server-side state (issue #56). The latest
// session wins: a retried Create must not bind resume/complete to a stale one.
func (m *MinioClient) FindUploadID(ctx context.Context, key string) (string, error) {
	core := minio.Core{Client: m.client}
	res, err := core.ListMultipartUploads(ctx, m.bucket, key, "", "", "", 1000)
	if err != nil {
		return "", fmt.Errorf("list multipart uploads %s: %w", key, err)
	}
	var latest string
	var initiated time.Time
	for _, u := range res.Uploads {
		if u.Key != key {
			continue
		}
		if latest == "" || u.Initiated.After(initiated) {
			latest = u.UploadID
			initiated = u.Initiated
		}
	}
	if latest == "" {
		return "", fmt.Errorf("multipart upload not found for %s", key)
	}
	return latest, nil
}

// ListParts returns uploaded parts ordered by part number.
func (m *MinioClient) ListParts(ctx context.Context, key, uploadID string) ([]MultipartPart, error) {
	core := minio.Core{Client: m.client}
	var parts []MultipartPart
	marker := 0
	for {
		res, err := core.ListObjectParts(ctx, m.bucket, key, uploadID, marker, 1000)
		if err != nil {
			return nil, fmt.Errorf("list parts %s: %w", key, err)
		}
		for _, p := range res.ObjectParts {
			parts = append(parts, MultipartPart{PartNumber: p.PartNumber, Size: p.Size, ETag: p.ETag})
		}
		if !res.IsTruncated || len(res.ObjectParts) == 0 {
			return parts, nil
		}
		marker = res.NextPartNumberMarker
	}
}

// CompleteMultipartUpload finishes the session with the listed parts.
func (m *MinioClient) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []MultipartPart) (int64, error) {
	if len(parts) == 0 {
		return 0, fmt.Errorf("no parts uploaded for %s", key)
	}
	cp := make([]minio.CompletePart, 0, len(parts))
	for _, p := range parts {
		cp = append(cp, minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	core := minio.Core{Client: m.client}
	info, err := core.CompleteMultipartUpload(ctx, m.bucket, key, uploadID, cp, minio.PutObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("complete multipart upload %s: %w", key, err)
	}
	return info.Size, nil
}

// PresignPart returns a presigned PUT URL for a single part; the browser
// uploads chunks directly to MinIO, the server never proxies part bytes.
func (m *MinioClient) PresignPart(ctx context.Context, key, uploadID string, partNumber int, expires time.Duration, publicEndpoint string) (string, error) {
	client, err := m.publicClient(publicEndpoint)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("partNumber", strconv.Itoa(partNumber))
	q.Set("uploadId", uploadID)
	u, err := client.Presign(ctx, http.MethodPut, m.bucket, key, expires, q)
	if err != nil {
		return "", fmt.Errorf("presign part %d %s: %w", partNumber, key, err)
	}
	return u.String(), nil
}
