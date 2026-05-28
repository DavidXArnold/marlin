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

func TestNGCName(t *testing.T) {
	n := NewNGC("")
	assert.Equal(t, "ngc", n.Name())
}

func TestNGCSearch(t *testing.T) {
	response := ngcSearchResponse{
		Results: []ngcResource{
			{Name: "nvidia/llama-3.1-8b-instruct", DisplayName: "Llama 3.1 8B", Description: "NVIDIA optimized"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.String(), "llama")
		assert.Contains(t, r.URL.String(), "CONTAINER")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	n := newNGCWithBase("", srv.URL)
	results, err := n.Search(context.Background(), "llama")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "nvcr.io/nim/nvidia/llama-3.1-8b-instruct:latest", results[0].ID)
	assert.Equal(t, "ngc", results[0].Registry)
}

func TestNimImageRef(t *testing.T) {
	cases := []struct {
		name, tag, want string
	}{
		{"nvidia/llama-3.1-8b-instruct", "", "nvcr.io/nim/nvidia/llama-3.1-8b-instruct:latest"},
		{"nvidia/llama-3.1-8b-instruct", "1.8", "nvcr.io/nim/nvidia/llama-3.1-8b-instruct:1.8"},
		{"nvcr.io/nim/meta/llama:latest", "", "nvcr.io/nim/meta/llama:latest"}, // passthrough
		{"meta/llama-3.1-8b", "2.0", "nvcr.io/nim/meta/llama-3.1-8b:2.0"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, nimImageRef(c.name, c.tag), "nimImageRef(%q, %q)", c.name, c.tag)
	}
}

func TestNGCSearchWithAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "ApiKey test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ngcSearchResponse{})
	}))
	defer srv.Close()

	n := newNGCWithBase("test-key", srv.URL)
	_, err := n.Search(context.Background(), "query")
	require.NoError(t, err)
}

func TestNGCSearchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	n := newNGCWithBase("", srv.URL)
	_, err := n.Search(context.Background(), "query")
	assert.Error(t, err)
}

func TestNGCSearchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	n := newNGCWithBase("", srv.URL)
	_, err := n.Search(context.Background(), "query")
	assert.Error(t, err)
}

func TestNGCFetchNotImplemented(t *testing.T) {
	n := NewNGC("")
	_, err := n.Fetch(context.Background(), "some/model")
	assert.Error(t, err)
}

func TestNGCSearchNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	n := newNGCWithBase("", srv.URL)
	_, err := n.Search(context.Background(), "query")
	assert.Error(t, err)
}

func newNGCWithBase(apiKey, base string) *NGC {
	n := NewNGC(apiKey)
	n.base = base
	return n
}
