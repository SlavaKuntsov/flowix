// Package client wraps HTTP calls from upload to the metadata service.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// metadataClientTimeout — исходящие вызовы в metadata (create/owner) —
// маленькие JSON, без таймаута клиент висел вечно (issue #63).
const metadataClientTimeout = 10 * time.Second

type MetadataClient struct {
	baseURL       string
	internalToken string
	client        *http.Client
}

func NewMetadataClient(baseURL, internalToken string) *MetadataClient {
	return &MetadataClient{baseURL: baseURL, internalToken: internalToken, client: &http.Client{Timeout: metadataClientTimeout}}
}

type CreateVideoResponse struct {
	ID string `json:"id"`
}

type internalVideoResponse struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id"`
}

func (c *MetadataClient) CreateVideo(token, title, description string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title, "description": description})
	req, err := http.NewRequest("POST", c.baseURL+"/api/v1/videos", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata create: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return "", fmt.Errorf("metadata create status %d", resp.StatusCode)
	}
	var out CreateVideoResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("empty video id")
	}
	return out.ID, nil
}

// StaleUpload is a video stuck in 'uploaded' as returned by metadata
// GET /internal/videos/stale (id + owner for re-publishing video.uploaded).
type StaleUpload struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id"`
}

// ListStaleUploaded returns videos still in 'uploaded' created more than
// olderThan ago — the reconciler filters out the ones that are genuinely done.
func (c *MetadataClient) ListStaleUploaded(olderThan time.Duration, limit int) ([]StaleUpload, error) {
	u := fmt.Sprintf("%s/internal/videos/stale?older_than_seconds=%d&limit=%d", c.baseURL, int(olderThan.Seconds()), limit)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	if c.internalToken != "" {
		req.Header.Set("X-Internal-Token", c.internalToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metadata stale uploads: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata stale uploads status %d", resp.StatusCode)
	}
	var out struct {
		Data []StaleUpload `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SetStatus updates a video status via the metadata internal API.
func (c *MetadataClient) SetStatus(videoID, status string) error {
	body, _ := json.Marshal(map[string]string{"status": status})
	req, err := http.NewRequest("PATCH", c.baseURL+"/internal/videos/"+url.PathEscape(videoID)+"/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.internalToken != "" {
		req.Header.Set("X-Internal-Token", c.internalToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("metadata set status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata set status %d", resp.StatusCode)
	}
	return nil
}

// GetVideoOwner resolves the owner of a video via metadata internal API
// (upload handlers must verify ownership before writing raw/{id}/* — issue #46).
func (c *MetadataClient) GetVideoOwner(videoID string) (string, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/internal/videos/"+url.PathEscape(videoID), nil)
	if err != nil {
		return "", err
	}
	if c.internalToken != "" {
		req.Header.Set("X-Internal-Token", c.internalToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata owner: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata owner status %d", resp.StatusCode)
	}
	var out internalVideoResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.OwnerID, nil
}
