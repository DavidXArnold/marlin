package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"})
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
		json.NewEncoder(w).Encode(githubRelease{TagName: "v0.0.7"})
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
		w.Write([]byte("not json"))
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
