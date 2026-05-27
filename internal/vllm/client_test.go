package vllm

import (
	"context"
	"encoding/json"
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
	c := NewClient("127.0.0.1", 19999, "")
	status, err := c.Health(context.Background())
	require.NoError(t, err, "unreachable server should not error, just return not ready")
	assert.False(t, status.Ready)
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
		json.NewEncoder(w).Encode(response)
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
	c := NewClient("unused", 0, "")
	c.base = "://invalid-url"
	_, err := c.Health(context.Background())
	assert.Error(t, err)
}

func TestModelsNoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ModelsResponse{Data: []Model{}})
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
		w.Write([]byte("not json"))
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

	c := NewClientFromBase(srv.URL, "tok")
	status, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Ready)
}

func clientFromTestServer(srv *httptest.Server) *Client {
	return clientFromTestServerWithKey(srv, "")
}

func clientFromTestServerWithKey(srv *httptest.Server, key string) *Client {
	c := NewClient("unused", 0, key)
	c.base = srv.URL
	return c
}
