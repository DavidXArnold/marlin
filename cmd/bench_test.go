package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// benchTestServer starts an httptest server with health, models, and streaming
// chat completions that emit the given tokens.
func benchTestServer(t *testing.T, tokens []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model","object":"model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, tok := range tokens {
				_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n", tok)
			}
			_, _ = fmt.Fprintln(w, "data: [DONE]")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// setBenchCfg writes a minimal config pointing at addr and sets cfgFile.
func setBenchCfg(t *testing.T, addr string) {
	t.Helper()
	host, port := "127.0.0.1", "8000"
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host = addr[:i]
			port = addr[i+1:]
			break
		}
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`[server]
host = %q
port = %s
health_path = "/health"

[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q
`, host, port, dir,
		filepath.Join(dir, "state.toml"),
		filepath.Join(dir, "secrets.env"),
		filepath.Join(dir, "model.env"),
	)
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o644))
	old := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = old })
}

func TestBenchCmdRegistered(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "bench" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestBenchModelNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	setBenchCfg(t, srv.Listener.Addr().String())

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().String("prompt", defaultBenchPrompt, "")
	cmd.Flags().Int("max-tokens", 256, "")
	cmd.Flags().Int("runs", 1, "")
	err := defaultRunBench(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}

func TestBenchNoModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()
	setBenchCfg(t, srv.Listener.Addr().String())

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().String("prompt", defaultBenchPrompt, "")
	cmd.Flags().Int("max-tokens", 256, "")
	cmd.Flags().Int("runs", 1, "")
	err := defaultRunBench(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no models")
}

func TestBenchSuccess(t *testing.T) {
	srv := benchTestServer(t, []string{"The", " water", " cycle"})
	defer srv.Close()
	setBenchCfg(t, srv.Listener.Addr().String())

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().String("prompt", defaultBenchPrompt, "")
	cmd.Flags().Int("max-tokens", 256, "")
	cmd.Flags().Int("runs", 1, "")
	err := defaultRunBench(cmd, nil)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "test-model")
	assert.Contains(t, out, "run 1/1")
}

func TestBenchInvalidRuns(t *testing.T) {
	srv := benchTestServer(t, nil)
	defer srv.Close()
	setBenchCfg(t, srv.Listener.Addr().String())

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().String("prompt", defaultBenchPrompt, "")
	cmd.Flags().Int("max-tokens", 256, "")
	cmd.Flags().Int("runs", 0, "")
	err := defaultRunBench(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--runs")
}

func TestBenchMultipleRuns(t *testing.T) {
	srv := benchTestServer(t, []string{"tok1", "tok2", "tok3"})
	defer srv.Close()
	setBenchCfg(t, srv.Listener.Addr().String())

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().String("prompt", defaultBenchPrompt, "")
	cmd.Flags().Int("max-tokens", 64, "")
	cmd.Flags().Int("runs", 2, "")
	err := defaultRunBench(cmd, nil)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "run 2/2")
	assert.Contains(t, out, "runs             : 2")
}
