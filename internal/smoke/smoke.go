// Package smoke provides post-startup API smoke tests for marlin.
package smoke

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

// Config controls which smoke tests are run.
type Config struct {
	Enabled bool
	Timeout time.Duration
	Skip    []string // test names to skip: "streaming", "tool_call"
}

// Result is the outcome of a smoke test suite.
type Result struct {
	Passed []string
	Failed []string
	Errors map[string]error
}

func (r *Result) OK() bool { return len(r.Failed) == 0 }

// doRequest is injectable for tests.
var doRequest = func(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

// Run executes all enabled smoke tests against the OpenAI-compatible endpoint
// at baseURL (e.g. "http://localhost:8000"). Results are printed to w.
func Run(ctx context.Context, baseURL string, cfg Config, w io.Writer) *Result {
	res := &Result{Errors: make(map[string]error)}
	skip := make(map[string]bool)
	for _, s := range cfg.Skip {
		skip[s] = true
	}

	runTest := func(name string, fn func(ctx context.Context) error) {
		if skip[name] {
			return
		}
		if err := fn(ctx); err != nil {
			res.Failed = append(res.Failed, name)
			res.Errors[name] = err
			_, _ = fmt.Fprintf(w, "  smoke %-16s FAIL  %v\n", name, err)
		} else {
			res.Passed = append(res.Passed, name)
			_, _ = fmt.Fprintf(w, "  smoke %-16s PASS\n", name)
		}
	}

	runTest("completion", func(ctx context.Context) error {
		return testCompletion(ctx, baseURL)
	})
	runTest("streaming", func(ctx context.Context) error {
		return testStreaming(ctx, baseURL)
	})
	runTest("tool_call", func(ctx context.Context) error {
		return testToolCall(ctx, baseURL)
	})

	return res
}

func testCompletion(ctx context.Context, base string) error {
	body := map[string]any{
		"model": "default",
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with the single word: ok"},
		},
		"max_tokens": 5,
	}
	resp, err := postJSON(ctx, base+"/v1/chat/completions", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if len(result.Choices) == 0 {
		return fmt.Errorf("no choices in response")
	}
	return nil
}

func testStreaming(ctx context.Context, base string) error {
	body := map[string]any{
		"model": "default",
		"messages": []map[string]string{
			{"role": "user", "content": "Say hi"},
		},
		"max_tokens": 5,
		"stream":     true,
	}
	resp, err := postJSON(ctx, base+"/v1/chat/completions", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	// Read at least one SSE chunk.
	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("read stream: %w", err)
	}
	chunk := string(buf[:n])
	if !strings.HasPrefix(chunk, "data:") {
		return fmt.Errorf("unexpected stream prefix: %q", chunk)
	}
	return nil
}

func testToolCall(ctx context.Context, base string) error {
	tool := map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "get_time",
			"description": "Returns the current time",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	body := map[string]any{
		"model": "default",
		"messages": []map[string]string{
			{"role": "user", "content": "What time is it? Use get_time."},
		},
		"tools":       []any{tool},
		"tool_choice": "auto",
		"max_tokens":  50,
	}
	resp, err := postJSON(ctx, base+"/v1/chat/completions", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// Tool call endpoint must not error — 200 OR 422 (model doesn't support) are both acceptable.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func postJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}
