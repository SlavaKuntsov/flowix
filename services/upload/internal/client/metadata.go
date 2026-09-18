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
