package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countingManifestServer(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		_, _ = w.Write([]byte(`{"profiles":{"foo":{"versions":{"1.0.0":{"schema_version":1,"sha256":"abc","path":"foo/1.0.0.toml"}}}}}`))
	}))
}

func TestLoadManifestFreshCacheHit(t *testing.T) {
	hits := 0
	srv := countingManifestServer(t, &hits)
	defer srv.Close()
	s := NewSourceWithBase("o", "r", "main", "profiles", srv.URL)

	cachePath := filepath.Join(t.TempDir(), "manifest.json")

	// First call populates the cache.
	_, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)

	// Second call within TTL must not hit the network again.
	_, err = LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

func TestLoadManifestStaleCacheRefetches(t *testing.T) {
	hits := 0
	srv := countingManifestServer(t, &hits)
	defer srv.Close()
	s := NewSourceWithBase("o", "r", "main", "profiles", srv.URL)

	cachePath := filepath.Join(t.TempDir(), "manifest.json")
	_, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)

	// A ttl of 0 makes any cache immediately stale.
	_, err = LoadManifest(context.Background(), s, cachePath, 0, false)
	require.NoError(t, err)
	assert.Equal(t, 2, hits)
}

func TestLoadManifestForceRefreshBypassesFreshCache(t *testing.T) {
	hits := 0
	srv := countingManifestServer(t, &hits)
	defer srv.Close()
	s := NewSourceWithBase("o", "r", "main", "profiles", srv.URL)

	cachePath := filepath.Join(t.TempDir(), "manifest.json")
	_, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)

	_, err = LoadManifest(context.Background(), s, cachePath, time.Hour, true)
	require.NoError(t, err)
	assert.Equal(t, 2, hits)
}

func TestLoadManifestMissingCacheTolerated(t *testing.T) {
	hits := 0
	srv := countingManifestServer(t, &hits)
	defer srv.Close()
	s := NewSourceWithBase("o", "r", "main", "profiles", srv.URL)

	cachePath := filepath.Join(t.TempDir(), "does-not-exist", "manifest.json")
	m, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, 1, hits)
}

func TestLoadManifestCorruptCacheTolerated(t *testing.T) {
	hits := 0
	srv := countingManifestServer(t, &hits)
	defer srv.Close()
	s := NewSourceWithBase("o", "r", "main", "profiles", srv.URL)

	cachePath := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, os.WriteFile(cachePath, []byte("not json"), 0o644))

	m, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, 1, hits)
}

func TestLoadManifestFetchFailureFallsBackToStaleCache(t *testing.T) {
	hits := 0
	srv := countingManifestServer(t, &hits)
	s := NewSourceWithBase("o", "r", "main", "profiles", srv.URL)

	cachePath := filepath.Join(t.TempDir(), "manifest.json")
	_, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)

	// Server goes away; force a refetch attempt (ttl=0 makes cache stale).
	srv.Close()
	m, err := LoadManifest(context.Background(), s, cachePath, 0, false)
	require.NoError(t, err, "should fall back to the stale cache rather than error")
	require.NotNil(t, m)
	assert.Contains(t, m.Profiles, "foo")
}

func TestLoadManifestFetchFailureNoCacheErrors(t *testing.T) {
	s := NewSourceWithBase("o", "r", "main", "profiles", "http://127.0.0.1:1")
	cachePath := filepath.Join(t.TempDir(), "manifest.json")

	_, err := LoadManifest(context.Background(), s, cachePath, time.Hour, false)
	require.Error(t, err)
}

func TestSaveAndLoadCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "manifest.json")
	c := &cachedManifest{
		FetchedAt: time.Now(),
		Manifest: Manifest{Profiles: map[string]ManifestProfile{
			"foo": {Versions: map[string]ManifestEntryVersion{"1.0.0": {SchemaVersion: 1}}},
		}},
	}
	require.NoError(t, saveCache(path, c))

	loaded := loadCache(path)
	require.NotNil(t, loaded)
	assert.Contains(t, loaded.Manifest.Profiles, "foo")
}

func TestLoadCacheMissingFile(t *testing.T) {
	assert.Nil(t, loadCache(filepath.Join(t.TempDir(), "nope.json")))
}

func TestSaveCacheMkdirAllError(t *testing.T) {
	// Put a regular file where the parent dir should be, so MkdirAll fails.
	base := t.TempDir()
	blocker := filepath.Join(base, "notadir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	err := saveCache(filepath.Join(blocker, "manifest.json"), &cachedManifest{})
	assert.Error(t, err)
}
