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

// KnownHealthPaths is the ordered list of well-known health endpoints probed
// when the model-derived primary path does not respond 200.
var KnownHealthPaths = []string{"/health", "/v1/health/live", "/v1/health/ready"}

// HealthProbe tries each path in order and returns the first that responds 200.
// Duplicate paths are skipped. Returns ("", false) when all paths fail or the
// server is unreachable.
func (c *Client) HealthProbe(ctx context.Context, paths ...string) (path string, ready bool) {
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+p, nil)
		if err != nil {
			return "", false
		}
		resp, err := c.http.Do(req)
		if err != nil {
			continue // server not yet up; try next path
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return p, true
		}
	}
	return "", false
}

// Health checks the configured health path. Use HealthProbe to try multiple
// known paths when the primary path may vary by model or container image.
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
// Reasoning carries thinking-phase text from models served with a
// --reasoning-parser, which vLLM streams separately from Content.
// PromptTokens is 0 on every chunk except the final usage-carrying one
// (requested via stream_options.include_usage), which has empty Choices.
type StreamChunk struct {
	Content      string
	Reasoning    string
	FinishReason string
	PromptTokens int
}

// ChatStream sends a streaming /v1/chat/completions request and calls fn for
// each received content chunk. The context deadline governs the entire stream.
// maxTokens caps the generated output; model selects the served model.
// stream_options.include_usage asks the server for a final usage-only chunk
// so callers can compute prefill throughput (prompt tokens / TTFT).
func (c *Client) ChatStream(ctx context.Context, model, prompt string, maxTokens int, fn func(StreamChunk) error) error {
	body, err := json.Marshal(map[string]any{
		"model":           model,
		"stream":          true,
		"max_tokens":      maxTokens,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"stream_options":  map[string]bool{"include_usage": true},
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

	// vLLM's field name for reasoning text differs by version/parser:
	// newer builds use "reasoning", older ones "reasoning_content".
	type delta struct {
		Content          string `json:"content"`
		Reasoning        string `json:"reasoning"`
		ReasoningContent string `json:"reasoning_content"`
	}
	type choice struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason"`
	}
	type usage struct {
		PromptTokens int `json:"prompt_tokens"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
		Usage   *usage   `json:"usage"`
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
		if jsonErr := json.Unmarshal([]byte(payload), &ch); jsonErr != nil {
			continue
		}
		if len(ch.Choices) == 0 && ch.Usage == nil {
			continue
		}
		var sc StreamChunk
		if len(ch.Choices) > 0 {
			d := ch.Choices[0].Delta
			sc.Content = d.Content
			sc.Reasoning = d.Reasoning
			if sc.Reasoning == "" {
				sc.Reasoning = d.ReasoningContent
			}
			sc.FinishReason = ch.Choices[0].FinishReason
		}
		if ch.Usage != nil {
			sc.PromptTokens = ch.Usage.PromptTokens
		}
		if callErr := fn(sc); callErr != nil {
			return callErr
		}
	}
	return scanner.Err()
}
