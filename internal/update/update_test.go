package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current   string
		candidate string
		want      bool
	}{
		{"0.0.7", "v0.0.8", true},
		{"v0.0.7", "v0.0.8", true},
		{"0.0.7", "0.0.7", false},
		{"0.0.8", "0.0.7", false},
		{"0.1.0", "1.0.0", true},
		{"1.0.0", "0.9.9", false},
		{"0.0.1", "0.1.0", true},
		{"0.9.9", "1.0.0", true},
		{"", "v0.0.8", false},
		{"0.0.7", "", false},
		{"not-a-version", "0.0.8", false},
		{"0.0.7", "not-a-version", false},
	}
	for _, c := range cases {
		t.Run(c.current+"<"+c.candidate, func(t *testing.T) {
			assert.Equal(t, c.want, IsNewer(c.current, c.candidate))
		})
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // the API fallback; the HEAD is the redirect probe
			assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"}))
	}))
	defer srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	latest, newer, err := Check(context.Background(), "0.0.1")
	require.NoError(t, err)
	assert.True(t, newer)
	assert.Equal(t, "v99.0.0", latest)
}

func TestCheck_AlreadyLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(githubRelease{TagName: "v0.0.7"}))
	}))
	defer srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	_, newer, err := Check(context.Background(), "0.0.7")
	require.NoError(t, err)
	assert.False(t, newer)
}

func TestCheck_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	_, _, err := Check(context.Background(), "0.0.1")
	assert.Error(t, err)
}

func TestCheck_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	_, _, err := Check(context.Background(), "0.0.1")
	assert.Error(t, err)
}

func TestCheck_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	_, _, err := Check(context.Background(), "0.0.1")
	assert.Error(t, err)
}

// redirectToServer returns a RoundTripper that rewrites any request to hit srv instead.
type redirectTransport struct{ base string }

func redirectToServer(srv *httptest.Server) http.RoundTripper {
	return redirectTransport{base: srv.URL}
}

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = req.URL.Host // preserve if already set
	// Replace the host with the test server host.
	u := *req.URL
	u.Scheme = "http"
	u.Host = t.base[len("http://"):]
	req2.URL = &u
	return http.DefaultTransport.RoundTrip(req2)
}

func TestAssetURL(t *testing.T) {
	url := AssetURL("v0.2.4", "linux", "amd64")
	assert.Equal(t, "https://github.com/DavidXArnold/marlin/releases/download/v0.2.4/marlin_0.2.4_linux_amd64.tar.gz", url)
}

func TestAssetURLNoLeadingV(t *testing.T) {
	url := AssetURL("0.2.4", "linux", "arm64")
	assert.Equal(t, "https://github.com/DavidXArnold/marlin/releases/download/0.2.4/marlin_0.2.4_linux_arm64.tar.gz", url)
}

func TestDownload(t *testing.T) {
	content := []byte("fake binary data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	dest := filepath.Join(t.TempDir(), "downloaded")
	require.NoError(t, Download(context.Background(), "https://example.com/asset", dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestDownloadServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	defer func() { HTTPClient = old }()

	dest := filepath.Join(t.TempDir(), "downloaded")
	err := Download(context.Background(), "https://example.com/asset", dest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestExtractBinary(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "fake.tar.gz")
	binContent := []byte("#!/bin/sh\necho marlin\n")
	require.NoError(t, writeFakeTarGz(archivePath, "marlin_0.2.4_linux_amd64/marlin", binContent))

	destPath := filepath.Join(t.TempDir(), "marlin")
	require.NoError(t, ExtractBinary(archivePath, "marlin", destPath))

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, binContent, got)

	info, err := os.Stat(destPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestExtractBinaryNotFound(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "fake.tar.gz")
	require.NoError(t, writeFakeTarGz(archivePath, "other-binary", []byte("data")))

	destPath := filepath.Join(t.TempDir(), "marlin")
	err := ExtractBinary(archivePath, "marlin", destPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func writeFakeTarGz(path, entryName string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name:     entryName,
		Typeflag: tar.TypeReg,
		Size:     int64(len(data)),
		Mode:     0o755,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err = tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}

// fakeGitHub serves both the web redirect and the REST API from one server.
// webTag == "" makes the redirect probe fail (no Location), forcing the API path.
type fakeGitHub struct {
	webTag, apiTag string
	apiStatus      int
	apiHits        atomic.Int32
	webHits        atomic.Int32
	authHeader     atomic.Value
}

func (f *fakeGitHub) install(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			f.apiHits.Add(1)
			f.authHeader.Store(r.Header.Get("Authorization"))
			if f.apiStatus != 0 {
				w.WriteHeader(f.apiStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(githubRelease{TagName: f.apiTag})
			return
		}
		f.webHits.Add(1)
		if f.webTag == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Location", "https://github.com/DavidXArnold/marlin/releases/tag/"+f.webTag)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	old := HTTPClient
	HTTPClient = &http.Client{Transport: redirectToServer(srv)}
	t.Cleanup(func() { HTTPClient = old })
}

func TestLatestTagPrefersRedirectOverAPI(t *testing.T) {
	f := &fakeGitHub{webTag: "v0.2.19", apiStatus: http.StatusForbidden}
	f.install(t)
	tag, err := LatestTag(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.2.19", tag)
	assert.Zero(t, f.apiHits.Load(), "API must not be touched when the redirect works")
}

func TestLatestTagFallsBackToAPIWithToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	f := &fakeGitHub{apiTag: "v0.2.19"}
	f.install(t)
	tag, err := LatestTag(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.2.19", tag)
	assert.Equal(t, "Bearer ghp_test", f.authHeader.Load())
}

func TestLatestTagRateLimitedErrorIsActionable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	f := &fakeGitHub{apiStatus: http.StatusForbidden}
	f.install(t)
	_, err := LatestTag(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}

func TestCheckCachedUsesFreshCacheWithoutNetwork(t *testing.T) {
	f := &fakeGitHub{webTag: "v0.2.19"}
	f.install(t)
	path := filepath.Join(t.TempDir(), "update-check.json")

	latest, newer, err := CheckCached(context.Background(), "0.2.18", path, time.Hour)
	require.NoError(t, err)
	assert.True(t, newer)
	assert.Equal(t, "v0.2.19", latest)
	assert.EqualValues(t, 1, f.webHits.Load())

	for range 5 {
		_, _, err = CheckCached(context.Background(), "0.2.18", path, time.Hour)
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, f.webHits.Load(), "cached runs must not hit the network")
}

func TestCheckCachedRefreshesWhenStale(t *testing.T) {
	f := &fakeGitHub{webTag: "v0.2.19"}
	f.install(t)
	path := filepath.Join(t.TempDir(), "update-check.json")
	_, _, err := CheckCached(context.Background(), "0.2.18", path, time.Hour)
	require.NoError(t, err)
	_, _, err = CheckCached(context.Background(), "0.2.18", path, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, f.webHits.Load())
}

func TestCheckCachedFallsBackToStaleCacheOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	stale, _ := json.Marshal(checkCache{CheckedAt: time.Now().Add(-48 * time.Hour), Latest: "v0.2.19"})
	require.NoError(t, os.WriteFile(path, stale, 0o644))

	t.Setenv("GITHUB_TOKEN", "")
	(&fakeGitHub{apiStatus: http.StatusForbidden}).install(t)

	latest, newer, err := CheckCached(context.Background(), "0.2.18", path, time.Hour)
	require.NoError(t, err, "a stale cached answer beats an error")
	assert.True(t, newer)
	assert.Equal(t, "v0.2.19", latest)
}

func TestCheckCachedErrorWithoutCache(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	(&fakeGitHub{apiStatus: http.StatusForbidden}).install(t)
	_, _, err := CheckCached(context.Background(), "0.2.18", filepath.Join(t.TempDir(), "x.json"), time.Hour)
	assert.Error(t, err)
}

func TestCheckCachedUpToDate(t *testing.T) {
	(&fakeGitHub{webTag: "v0.2.18"}).install(t)
	_, newer, err := CheckCached(context.Background(), "0.2.18", filepath.Join(t.TempDir(), "x.json"), time.Hour)
	require.NoError(t, err)
	assert.False(t, newer)
}
