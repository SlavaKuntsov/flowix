package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type MetadataClient struct {
	baseURL string
	client  *http.Client
}

func NewMetadataClient(baseURL string) *MetadataClient {
	return &MetadataClient{baseURL: baseURL, client: &http.Client{}}
}

type CreateVideoResponse struct {
	ID string `json:"id"`
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
