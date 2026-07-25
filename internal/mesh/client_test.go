package mesh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientRuntimeOK(t *testing.T) {
	payload := RuntimeInfo{
		Peers: []PeerInfo{
			{ID: "abc", Addr: "192.168.1.2", Models: []string{"qwen3-8b"}},
		},
		Models: []ModelInfo{
			{Ref: "/models/my.gguf", State: "ready"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/runtime", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	info, err := client.Runtime(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Len(t, info.Peers, 1)
	assert.Equal(t, "abc", info.Peers[0].ID)
	assert.Len(t, info.Models, 1)
	assert.Equal(t, "ready", info.Models[0].State)
}

func TestClientRuntimeNotRunning(t *testing.T) {
	// Point at a port nothing is listening on.
	client := NewClient("http://127.0.0.1:19337")
	info, err := client.Runtime(context.Background())
	assert.NoError(t, err) // connection refused → nil, nil
	assert.Nil(t, info)
}

func TestClientRuntimeBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Runtime(context.Background())
	assert.Error(t, err)
}

func TestClientLoadModelOK(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/runtime/models", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := NewClient(srv.URL).LoadModel(context.Background(), "/models/my.gguf")
	require.NoError(t, err)
	assert.Equal(t, "/models/my.gguf", gotBody["model"])
}

func TestClientLoadModelError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad model ref"))
	}))
	defer srv.Close()

	err := NewClient(srv.URL).LoadModel(context.Background(), "bad")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad model ref")
}

func TestClientUnloadModelOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/api/runtime/models/")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := NewClient(srv.URL).UnloadModel(context.Background(), "my-model")
	require.NoError(t, err)
}

func TestClientUnloadModelNotFoundIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := NewClient(srv.URL).UnloadModel(context.Background(), "gone")
	assert.NoError(t, err)
}

func TestClientApplyConfigOK(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/runtime/control/apply-config", r.URL.Path)
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := NewClient(srv.URL).ApplyConfig(context.Background())
	require.NoError(t, err)
	assert.True(t, called)
}

func TestClientApplyConfigNotRunning(t *testing.T) {
	// Should be a silent no-op when mesh-llm isn't running.
	err := NewClient("http://127.0.0.1:19337").ApplyConfig(context.Background())
	assert.NoError(t, err)
}

func TestClientIsRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"peers":[],"models":[]}`))
	}))
	defer srv.Close()

	assert.True(t, NewClient(srv.URL).IsRunning(context.Background()))
	assert.False(t, NewClient("http://127.0.0.1:19337").IsRunning(context.Background()))
}
