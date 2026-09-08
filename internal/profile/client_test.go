package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceURLConstruction(t *testing.T) {
	s := NewSourceWithBase("owner", "repo", "main", "profiles", "https://example.test")
	assert.Equal(t, "https://example.test/owner/repo/main/profiles/manifest.json", s.manifestURL())
	assert.Equal(t, "https://example.test/owner/repo/main/profiles/foo/1.0.0.toml", s.fileURL("foo/1.0.0.toml"))
}

func TestFetchManifest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/owner/repo/main/profiles/manifest.json", r.URL.Path)
		_, _ = w.Write([]byte(`{"profiles":{"foo":{"versions":{"1.0.0":{"schema_version":1,"sha256":"abc","path":"foo/1.0.0.toml"}}}}}`))
	}))
	defer srv.Close()

	s := NewSourceWithBase("owner", "repo", "main", "profiles", srv.URL)
	m, err := s.FetchManifest(context.Background())
	require.NoError(t, err)
	require.Contains(t, m.Profiles, "foo")
	assert.Equal(t, 1, m.Profiles["foo"].Versions["1.0.0"].SchemaVersion)
}

func TestFetchManifestServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404: Not Found"))
	}))
	defer srv.Close()

	s := NewSourceWithBase("owner", "repo", "main", "profiles", srv.URL)
	_, err := s.FetchManifest(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestFetchManifestTruncatesLongErrorBody(t *testing.T) {
	longBody := strings.Repeat("x", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(longBody))
	}))
	defer srv.Close()

	s := NewSourceWithBase("owner", "repo", "main", "profiles", srv.URL)
	_, err := s.FetchManifest(context.Background())
	require.Error(t, err)
	assert.Less(t, len(err.Error()), len(longBody))
	assert.Contains(t, err.Error(), "...")
}

func TestFetchManifestInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	s := NewSourceWithBase("owner", "repo", "main", "profiles", srv.URL)
	_, err := s.FetchManifest(context.Background())
	require.Error(t, err)
}

func TestFetchProfileBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/owner/repo/main/profiles/foo/1.0.0.toml", r.URL.Path)
		_, _ = w.Write([]byte("[model]\nid = \"x\"\n"))
	}))
	defer srv.Close()

	s := NewSourceWithBase("owner", "repo", "main", "profiles", srv.URL)
	body, err := s.FetchProfileBytes(context.Background(), "foo/1.0.0.toml")
	require.NoError(t, err)
	assert.Contains(t, string(body), "id = \"x\"")
}

func TestSetVerboseLogging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"profiles":{}}`))
	}))
	defer srv.Close()

	var buf strings.Builder
	s := NewSourceWithBase("owner", "repo", "main", "profiles", srv.URL)
	s.SetVerbose(&buf, 3)
	_, err := s.FetchManifest(context.Background())
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "GET ")
	assert.Contains(t, buf.String(), "status: 200")
	assert.Contains(t, buf.String(), "response body:")
}

func TestFetchManifestNetworkError(t *testing.T) {
	s := NewSourceWithBase("owner", "repo", "main", "profiles", "http://127.0.0.1:1")
	_, err := s.FetchManifest(context.Background())
	require.Error(t, err)
}
