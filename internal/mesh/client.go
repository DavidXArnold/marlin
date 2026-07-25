package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the mesh-llm local management API (default: localhost:3131).
type Client struct {
	base string
	http *http.Client
}

// NewClient returns a management API client for the given base URL,
// e.g. "http://localhost:3131".
func NewClient(baseURL string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// RuntimeInfo is the node+mesh state returned by GET /api/runtime.
type RuntimeInfo struct {
	Peers  []PeerInfo  `json:"peers"`
	Models []ModelInfo `json:"models"`
}

// PeerInfo describes a connected mesh peer.
type PeerInfo struct {
	ID     string   `json:"id"`
	Addr   string   `json:"addr"`
	Models []string `json:"models"`
}

// ModelInfo describes a model loaded on this node.
type ModelInfo struct {
	Ref   string `json:"ref"`
	State string `json:"state"`
}

// Runtime fetches the current node+mesh status from GET /api/runtime.
// Returns (nil, nil) when mesh-llm is not reachable — callers treat nil as absent.
func (c *Client) Runtime(ctx context.Context) (*RuntimeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/runtime", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil // connection refused / not running
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mesh-llm /api/runtime: HTTP %d", resp.StatusCode)
	}
	var info RuntimeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding mesh-llm runtime response: %w", err)
	}
	return &info, nil
}

// LoadModel asks mesh-llm to load modelRef via POST /api/runtime/models.
func (c *Client) LoadModel(ctx context.Context, modelRef string) error {
	body, _ := json.Marshal(map[string]string{"model": modelRef})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/runtime/models", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mesh-llm load model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mesh-llm load model: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// UnloadModel asks mesh-llm to unload modelRef via DELETE /api/runtime/models/<ref>.
// A 404 is treated as success (model already unloaded).
func (c *Client) UnloadModel(ctx context.Context, modelRef string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.base+"/api/runtime/models/"+modelRef, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mesh-llm unload model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mesh-llm unload model: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ApplyConfig asks a running mesh-llm peer to reload its config from disk
// via POST /api/runtime/control/apply-config.
// Returns nil when mesh-llm is not running (soft no-op).
func (c *Client) ApplyConfig(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/runtime/control/apply-config", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil // not running — patch will take effect at next start
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mesh-llm apply-config: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// IsRunning reports whether the mesh-llm management API is reachable.
func (c *Client) IsRunning(ctx context.Context) bool {
	info, err := c.Runtime(ctx)
	return err == nil && info != nil
}
