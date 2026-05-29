package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNGCName(t *testing.T) {
	n := NewNGC("")
	assert.Equal(t, "ngc", n.Name())
}

func TestNGCSearch(t *testing.T) {
	response := nimModelsResponse{
		Data: []nimModel{
			{ID: "nvidia/llama-3.1-8b-instruct", Created: 1700000000, OwnedBy: "nvidia"},
			{ID: "meta/llama-3.2-1b-instruct", Created: 1700000001, OwnedBy: "meta"},
			{ID: "nvidia/mistral-7b", Created: 1700000002, OwnedBy: "nvidia"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer srv.Close()

	n := newNGCWithBase("", srv.URL)
	results, err := n.Search(context.Background(), "llama")
	require.NoError(t, err)
	require.Len(t, results, 2) // only the two llama models
	assert.Equal(t, "nvcr.io/nim/nvidia/llama-3.1-8b-instruct:latest", results[0].ID)
	assert.Equal(t, "ngc", results[0].Registry)
}

func TestNGCSearchReturnsUpTo20(t *testing.T) {
	models := make([]nimModel, 30)
	for i := range models {
		models[i] = nimModel{ID: "nvidia/llama-match", Created: 1700000000 + int64(i), OwnedBy: "nvidia"}
	}
	response := nimModelsResponse{Data: models}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer srv.Close()

	n := newNGCWithBase("", srv.URL)
	results, err := n.Search(context.Background(), "llama")
	require.NoError(t, err)
	assert.Len(t, results, 20)
}

func TestNGCModelToModelInfo(t *testing.T) {
	info := nimModel{
		ID:      "meta/llama-3.1-8b-instruct",
		Created: 1704153600,
		OwnedBy: "meta",
	}.toModelInfo()

	assert.Equal(t, "nvcr.io/nim/meta/llama-3.1-8b-instruct:latest", info.ID)
	assert.Equal(t, "ngc", info.Registry)
	assert.False(t, info.LastUpdated.IsZero())
}

func TestNGCModelToModelInfoZeroCreated(t *testing.T) {
	info := nimModel{ID: "meta/llama", Created: 0}.toModelInfo()
	assert.True(t, info.LastUpdated.IsZero())
}

func TestNGCModelToModelInfoPreNIMTimestamp(t *testing.T) {
	// NVIDIA API returns sub-nimEpoch values for many entries; these should be ignored.
	info := nimModel{ID: "meta/llama", Created: 735790403}.toModelInfo()
	assert.True(t, info.LastUpdated.IsZero())
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
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(nimModelsResponse{}))
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
	assert.Contains(t, err.Error(), "authentication required")
}

func TestNGCSearchBadKeyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	n := newNGCWithBase("bad-key", srv.URL)
	_, err := n.Search(context.Background(), "query")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marlin configure")
}

func TestNGCSearchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	n := newNGCWithBase("", srv.URL)
	_, err := n.Search(context.Background(), "query")
	assert.Error(t, err)
}

func TestNGCVerboseLogging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(nimModelsResponse{}))
	}))
	defer srv.Close()

	var buf strings.Builder
	n := newNGCWithBase("key", srv.URL)
	n.SetVerbose(&buf, 2)
	_, err := n.Search(context.Background(), "llama")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "GET ")
	assert.Contains(t, buf.String(), "status: 200")
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
