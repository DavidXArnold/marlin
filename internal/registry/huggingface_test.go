package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHuggingFaceName(t *testing.T) {
	hf := NewHuggingFace("")
	assert.Equal(t, "huggingface", hf.Name())
}

func TestHuggingFaceSearch(t *testing.T) {
	models := []hfModel{
		{ID: "Qwen/Qwen2.5-72B-Instruct-AWQ", Private: false, Description: "Qwen 72B AWQ"},
		{ID: "meta-llama/Llama-3.1-8B-Instruct", Private: false},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.String(), "search=qwen")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	results, err := hf.Search(context.Background(), "qwen")
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "Qwen/Qwen2.5-72B-Instruct-AWQ", results[0].ID)
	assert.Equal(t, "huggingface", results[0].Registry)
	assert.Equal(t, "Qwen 72B AWQ", results[0].Description)
}

func TestHuggingFaceSearchWithToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]hfModel{})
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("test-token", srv.URL)
	_, err := hf.Search(context.Background(), "query")
	require.NoError(t, err)
}

func TestHuggingFaceSearchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Search(context.Background(), "query")
	assert.Error(t, err)
}

func TestHuggingFaceSearchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Search(context.Background(), "query")
	assert.Error(t, err)
}

func TestHuggingFaceFetch(t *testing.T) {
	model := hfModel{ID: "Qwen/Qwen2.5-72B-Instruct-AWQ", Private: false, Description: "desc"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "Qwen")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(model)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	info, err := hf.Fetch(context.Background(), "Qwen/Qwen2.5-72B-Instruct-AWQ")
	require.NoError(t, err)
	assert.Equal(t, "Qwen/Qwen2.5-72B-Instruct-AWQ", info.ID)
}

func TestHuggingFaceFetchNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Fetch(context.Background(), "nonexistent/model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHuggingFaceFetchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Fetch(context.Background(), "some/model")
	assert.Error(t, err)
}

func TestHuggingFaceSearchNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed immediately — any request will fail

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Search(context.Background(), "query")
	assert.Error(t, err)
}

func TestHuggingFaceFetchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Fetch(context.Background(), "some/model")
	assert.Error(t, err)
}

func TestHuggingFaceFetchNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Fetch(context.Background(), "some/model")
	assert.Error(t, err)
}

// newHuggingFaceWithBase creates a client pointing at a test server URL.
func newHuggingFaceWithBase(token, base string) *HuggingFace {
	hf := NewHuggingFace(token)
	hf.base = base
	return hf
}
