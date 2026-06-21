package smoke

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestRunAllPass(t *testing.T) {
	completionResp := `{"choices":[{"message":{"content":"ok"}}]}`
	streamResp := "data: {\"id\":\"1\",\"choices\":[]}\n\n"

	var calls []string
	old := doRequest
	doRequest = func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Path)
		path := req.URL.Path
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(string(body), `"stream":true`) {
			return mockResponse(200, streamResp), nil
		}
		if strings.Contains(string(path), "chat") {
			return mockResponse(200, completionResp), nil
		}
		return mockResponse(200, completionResp), nil
	}
	t.Cleanup(func() { doRequest = old })

	var buf bytes.Buffer
	cfg := Config{Enabled: true, Timeout: 5 * time.Second}
	res := Run(context.Background(), "http://localhost:8000", cfg, &buf)
	require.NotNil(t, res)
	assert.True(t, res.OK(), "all tests should pass, failed: %v", res.Failed)
	assert.Len(t, res.Passed, 3)
	assert.Contains(t, buf.String(), "PASS")
}

func TestRunCompletionFail(t *testing.T) {
	old := doRequest
	doRequest = func(req *http.Request) (*http.Response, error) {
		return mockResponse(500, `{"error":"internal"}`), nil
	}
	t.Cleanup(func() { doRequest = old })

	var buf bytes.Buffer
	cfg := Config{Enabled: true, Skip: []string{"streaming", "tool_call"}}
	res := Run(context.Background(), "http://localhost:8000", cfg, &buf)
	assert.False(t, res.OK())
	assert.Contains(t, res.Failed, "completion")
	assert.Contains(t, buf.String(), "FAIL")
}

func TestRunSkip(t *testing.T) {
	completionResp := `{"choices":[{"message":{"content":"ok"}}]}`

	old := doRequest
	doRequest = func(req *http.Request) (*http.Response, error) {
		return mockResponse(200, completionResp), nil
	}
	t.Cleanup(func() { doRequest = old })

	var buf bytes.Buffer
	cfg := Config{Enabled: true, Skip: []string{"streaming", "tool_call"}}
	res := Run(context.Background(), "http://localhost:8000", cfg, &buf)
	assert.True(t, res.OK())
	assert.Len(t, res.Passed, 1)
	assert.Contains(t, res.Passed, "completion")
}

func TestRunToolCallAllowed422(t *testing.T) {
	old := doRequest
	doRequest = func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(string(body), `"stream":true`) {
			return mockResponse(200, "data: {}\n"), nil
		}
		if strings.Contains(string(body), "get_time") {
			return mockResponse(422, `{"detail":"not supported"}`), nil
		}
		return mockResponse(200, `{"choices":[{"message":{"content":"ok"}}]}`), nil
	}
	t.Cleanup(func() { doRequest = old })

	var buf bytes.Buffer
	cfg := Config{Enabled: true}
	res := Run(context.Background(), "http://localhost:8000", cfg, &buf)
	assert.True(t, res.OK(), "422 on tool_call should be treated as acceptable")
}

func TestResultOK(t *testing.T) {
	r := &Result{Errors: make(map[string]error)}
	assert.True(t, r.OK())
	r.Failed = []string{"something"}
	assert.False(t, r.OK())
}
