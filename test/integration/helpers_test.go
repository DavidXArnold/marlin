//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/DavidXArnold/marlin/internal/config"
)

// testEnv is a self-contained filesystem environment for a single integration test.
type testEnv struct {
	dir       string
	modelsDir string
	cfgPath   string
	cfg       *config.Config
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("creating models dir: %v", err)
	}

	cfg := config.Defaults()
	cfg.Paths.ModelsDir = modelsDir
	cfg.Paths.ActiveSymlink = filepath.Join(dir, "model.env")
	cfg.Paths.SecretsEnv = filepath.Join(dir, "secrets.env")
	cfg.Paths.StateFile = filepath.Join(dir, "state.toml")
	cfg.Service.SystemdUnit = "marlin-nonexistent-test-unit"

	cfgPath := filepath.Join(dir, "config.toml")
	f, err := os.Create(cfgPath)
	if err != nil {
		t.Fatalf("creating config file: %v", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		t.Fatalf("encoding config: %v", err)
	}

	return &testEnv{dir: dir, modelsDir: modelsDir, cfgPath: cfgPath, cfg: cfg}
}

// addModel writes a valid vLLM model config to the testEnv's models dir.
func (e *testEnv) addModel(t *testing.T, slug string) {
	t.Helper()
	mc := &config.ModelConfig{
		Model: config.ModelMeta{
			Type:     config.ProviderVLLM,
			ID:       "Qwen/Qwen2.5-72B-Instruct-AWQ",
			Registry: "huggingface",
			Status:   config.StatusWorking,
		},
		Serve: config.ServeConfig{
			Quantization:         "awq_marlin",
			GPUMemoryUtilization: 0.92,
			MaxModelLen:          32768,
			ServedModelName:      []string{e.cfg.Server.Alias},
			ToolCallParser:       "hermes",
		},
	}
	path := filepath.Join(e.modelsDir, slug+".toml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating model config %s: %v", slug, err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(mc); err != nil {
		t.Fatalf("encoding model config %s: %v", slug, err)
	}
}

// mockVLLMServer starts a minimal OpenAI-compatible API mock server.
// Tests use this when MARLIN_TEST_HOST is not set.
func mockVLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "Qwen/Qwen2.5-72B-Instruct-AWQ", "object": "model", "created": 0},
					{"id": "meta-llama/Llama-3.1-8B-Instruct", "object": "model", "created": 0},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
