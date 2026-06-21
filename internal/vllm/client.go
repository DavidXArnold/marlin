package vllm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base       string
	apiKey     string
	healthPath string
	http       *http.Client
}

func NewClient(host string, port int, apiKey, healthPath string) *Client {
	return &Client{
		base:       fmt.Sprintf("http://%s:%d", host, port),
		apiKey:     apiKey,
		healthPath: healthPath,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClientFromBase creates a Client with a pre-formatted base URL.
// Useful when the URL is already known (e.g. httptest.Server.URL in integration tests).
func NewClientFromBase(base, apiKey, healthPath string) *Client {
	return &Client{
		base:       base,
		apiKey:     apiKey,
		healthPath: healthPath,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

type HealthStatus struct {
	Ready bool
}

func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+c.healthPath, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &HealthStatus{Ready: false}, nil
	}
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing models: unexpected status %d", resp.StatusCode)
	}

	var mr ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("listing models: decoding response: %w", err)
	}

	return mr.Data, nil
}

// StreamChunk holds one SSE event's token content and finish reason.
type StreamChunk struct {
	Content      string
	FinishReason string
}

// ChatStream sends a streaming /v1/chat/completions request and calls fn for
// each received content chunk. The context deadline governs the entire stream.
// maxTokens caps the generated output; model selects the served model.
func (c *Client) ChatStream(ctx context.Context, model, prompt string, maxTokens int, fn func(StreamChunk) error) error {
	body, err := json.Marshal(map[string]any{
		"model":     model,
		"stream":    true,
		"max_tokens": maxTokens,
		"messages":  []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Use a client without timeout for streaming — the context deadline governs.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("chat completions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat completions: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	type delta struct {
		Content string `json:"content"`
	}
	type choice struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := line[len("data: "):]
		if payload == "[DONE]" {
			break
		}
		var ch chunk
		if jsonErr := json.Unmarshal([]byte(payload), &ch); jsonErr != nil || len(ch.Choices) == 0 {
			continue
		}
		sc := StreamChunk{
			Content:      ch.Choices[0].Delta.Content,
			FinishReason: ch.Choices[0].FinishReason,
		}
		if callErr := fn(sc); callErr != nil {
			return callErr
		}
	}
	return scanner.Err()
}
