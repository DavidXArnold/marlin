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

func TestParseHFTime(t *testing.T) {
	cases := []struct {
		input    string
		wantZero bool
	}{
		{"2024-10-10T07:41:18.000Z", false},  // fractional seconds
		{"2024-10-10T07:41:18Z", false},      // no fractional seconds
		{"2024-10-10T07:41:18+00:00", false}, // RFC3339 with offset
		{"", true},
		{"not-a-date", true},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got := parseHFTime(c.input)
			if c.wantZero {
				assert.True(t, got.IsZero(), "expected zero time for %q", c.input)
			} else {
				assert.False(t, got.IsZero(), "expected non-zero time for %q", c.input)
			}
		})
	}
}

func TestHuggingFaceSearchFullParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.String(), "full=true")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]hfModel{})
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	_, err := hf.Search(context.Background(), "llama")
	require.NoError(t, err)
}

func TestHuggingFaceSearchWithMetadata(t *testing.T) {
	models := []hfModel{
		{
			ID:           "Qwen/Qwen2.5-7B-AWQ",
			LastModified: "2024-10-10T07:41:18.000Z",
			Tags:         []string{"awq", "7b"},
			SafeTensors:  &hfSafeTensors{Total: 7_000_000_000},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	results, err := hf.Search(context.Background(), "qwen")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].LastUpdated.IsZero(), "LastUpdated should be populated")
	assert.InDelta(t, 7.0, results[0].ParamsBillion, 0.01)
	assert.Equal(t, "awq", results[0].Quantization)
}

func TestHuggingFaceSearchFallbackToCreatedAt(t *testing.T) {
	models := []hfModel{
		{ID: "some/model", CreatedAt: "2024-01-15T10:00:00.000Z"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models)
	}))
	defer srv.Close()

	hf := newHuggingFaceWithBase("", srv.URL)
	results, err := hf.Search(context.Background(), "model")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].LastUpdated.IsZero(), "should fall back to createdAt")
}
