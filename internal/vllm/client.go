package vllm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	base   string
	apiKey string
	http   *http.Client
}

func NewClient(host string, port int, apiKey string) *Client {
	return &Client{
		base:   fmt.Sprintf("http://%s:%d", host, port),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClientFromBase creates a Client with a pre-formatted base URL.
// Useful when the URL is already known (e.g. httptest.Server.URL in integration tests).
func NewClientFromBase(base, apiKey string) *Client {
	return &Client{
		base:   base,
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

type HealthStatus struct {
	Ready bool
}

func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &HealthStatus{Ready: false}, nil
	}
	defer resp.Body.Close()

	return &HealthStatus{Ready: resp.StatusCode == http.StatusOK}, nil
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
}

type ModelsResponse struct {
	Data []Model `json:"data"`
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing models: unexpected status %d", resp.StatusCode)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("listing models: decoding response: %w", err)
	}

	return mr.Data, nil
}
