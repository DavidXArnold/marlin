package vllm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	status, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Ready)
}

func TestHealthNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	status, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Ready)
}

func TestHealthUnreachable(t *testing.T) {
	c := NewClient("127.0.0.1", 19999, "", "/health")
	status, err := c.Health(context.Background())
	require.NoError(t, err, "unreachable server should not error, just return not ready")
	assert.False(t, status.Ready)
}

func TestHealthProbePrimaryHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	path, ready := c.HealthProbe(context.Background(), "/health", "/v1/health/live")
	assert.True(t, ready)
	assert.Equal(t, "/health", path)
}

func TestHealthProbeFallback(t *testing.T) {
	// Primary path returns 404; fallback /v1/health/live returns 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health/live" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	path, ready := c.HealthProbe(context.Background(), "/health", "/v1/health/live")
	assert.True(t, ready)
	assert.Equal(t, "/v1/health/live", path)
}

func TestHealthProbeAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	path, ready := c.HealthProbe(context.Background(), "/health", "/v1/health/live")
	assert.False(t, ready)
	assert.Empty(t, path)
}

func TestHealthProbeDeduplicates(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	path, ready := c.HealthProbe(context.Background(), "/health", "/health", "/health")
	assert.True(t, ready)
	assert.Equal(t, "/health", path)
	assert.Equal(t, 1, callCount, "duplicate paths must not be probed more than once")
}

func TestHealthProbeUnreachable(t *testing.T) {
	c := NewClient("127.0.0.1", 19999, "", "/health")
	path, ready := c.HealthProbe(context.Background(), "/health", "/v1/health/live")
	assert.False(t, ready)
	assert.Empty(t, path)
}

func TestModels(t *testing.T) {
	response := ModelsResponse{
		Data: []Model{
			{ID: "gn100", Object: "model"},
			{ID: "qwen25-72b", Object: "model"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer srv.Close()

	c := clientFromTestServerWithKey(srv, "test-key")
	models, err := c.Models(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "gn100", models[0].ID)
}

func TestModelsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	_, err := c.Models(context.Background())
	assert.Error(t, err)
}

func TestHealthInvalidURL(t *testing.T) {
	c := NewClient("unused", 0, "", "/health")
	c.base = "://invalid-url"
	_, err := c.Health(context.Background())
	assert.Error(t, err)
}

func TestModelsNoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(ModelsResponse{Data: []Model{}}))
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	models, err := c.Models(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestModelsNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := clientFromTestServer(srv)
	_, err := c.Models(context.Background())
	assert.Error(t, err)
}

func TestModelsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	_, err := c.Models(context.Background())
	assert.Error(t, err)
}

func TestNewClientFromBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClientFromBase(srv.URL, "tok", "/health")
	status, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Ready)
}

func TestHealthCustomPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClientFromBase(srv.URL, "", "/healthz")
	status, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Ready)

	// Default /health path returns 404 on this server → not ready.
	c2 := NewClientFromBase(srv.URL, "", "/health")
	status2, err := c2.Health(context.Background())
	require.NoError(t, err)
	assert.False(t, status2.Ready)
}

func TestChatStreamOK(t *testing.T) {
	events := []string{
		`data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, e := range events {
			_, _ = w.Write([]byte(e + "\n"))
		}
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	var got []string
	err := c.ChatStream(context.Background(), "llama", "hi", 64, func(sc StreamChunk) error {
		if sc.Content != "" {
			got = append(got, sc.Content)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello", " world"}, got)
}

func TestChatStreamServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("model overloaded"))
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	err := c.ChatStream(context.Background(), "m", "hi", 64, func(_ StreamChunk) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestChatStreamCallbackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"tok"}}]}` + "\n"))
		_, _ = w.Write([]byte(`data: [DONE]` + "\n"))
	}))
	defer srv.Close()

	c := clientFromTestServer(srv)
	err := c.ChatStream(context.Background(), "m", "hi", 64, func(_ StreamChunk) error {
		return fmt.Errorf("stop early")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop early")
}

func clientFromTestServer(srv *httptest.Server) *Client {
	return clientFromTestServerWithKey(srv, "")
}

func clientFromTestServerWithKey(srv *httptest.Server, key string) *Client {
	c := NewClient("unused", 0, key, "/health")
	c.base = srv.URL
	return c
}
