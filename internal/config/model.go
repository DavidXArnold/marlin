package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// BundledModels is an optional embedded FS containing factory model profiles.
// When set, resolveChain falls back to it when a slug is not found on disk.
// Files must be at "models/<slug>.toml" within the FS.
// Set by the application entry point (cmd package).
var BundledModels fs.FS


type ModelStatus string
type ProviderType string

const (
	StatusWorking  ModelStatus = "working"
	StatusBroken   ModelStatus = "broken"
	StatusUntested ModelStatus = "untested"

	ProviderVLLM     ProviderType = "vllm"
	ProviderNIM      ProviderType = "nim"
	ProviderLlamaCpp ProviderType = "llamacpp"
	ProviderMesh     ProviderType = "mesh"
)

type ModelConfig struct {
	Model ModelMeta   `toml:"model"`
	Serve ServeConfig `toml:"serve"`
}

type ModelMeta struct {
	Type     ProviderType `toml:"type"`
	ID       string       `toml:"id"`
	Image    string       `toml:"image"`    // NIM only: nvcr.io/nim/<org>/<model>:tag
	Registry string       `toml:"registry"` // vLLM only: huggingface, ngc
	Status   ModelStatus  `toml:"status"`
	Notes    string       `toml:"notes"`
	Extends  string       `toml:"extends"`  // slug of parent model to inherit from
	Abstract bool         `toml:"abstract"` // hide from picker and list; use as base only
}

type ServeConfig struct {
	Quantization         string   `toml:"quantization"`
	ToolCallParser       string   `toml:"tool_call_parser"`
	ServedModelName      []string `toml:"served_model_name"`
	GPUMemoryUtilization float64  `toml:"gpu_memory_utilization"`
	MaxModelLen          int      `toml:"max_model_len"`
	ExtraFlags           []string `toml:"extra_flags"`   // vLLM: extra CLI flags passed to vllm serve
	ExtraEnv             []string `toml:"extra_env"`     // NIM: extra KEY=VALUE env vars for the container
	ExtraVolumes         []string `toml:"extra_volumes"` // NIM: extra /host:/container volume mounts

	// llama.cpp-specific fields
	GGUFPath    string `toml:"gguf_path"`    // path to the .gguf model file
	NGL         int    `toml:"ngl"`          // GPU layers to offload (-ngl flag)
	ContextSize int    `toml:"context_size"` // context window size (-c flag)

	// vLLM optional flags
	TrustRemoteCode bool `toml:"trust_remote_code"` // pass --trust-remote-code to vllm serve

	// Health check endpoint override. When empty, DefaultHealthPath(provider) is used.
	HealthPath string `toml:"health_path"`
}

// DefaultHealthPath returns the well-known health endpoint for a provider type.
// NIM containers expose /v1/health/live; everything else uses /health.
func DefaultHealthPath(t ProviderType) string {
	if t == ProviderNIM {
		return "/v1/health/live"
	}
	return "/health"
}

// EffectiveHealthPath returns the health endpoint to use for m.
// Priority: model-explicit health_path > provider default > fallback.
// fallback is used only when m is nil (no active model loaded).
func EffectiveHealthPath(m *ModelConfig, fallback string) string {
	if m == nil {
		if fallback != "" {
			return fallback
		}
		return "/health"
	}
	if m.Serve.HealthPath != "" {
		return m.Serve.HealthPath
	}
	return DefaultHealthPath(m.Model.Type)
}

// IsBundled reports whether slug names a bundled model profile embedded in the binary.
func IsBundled(slug string) bool {
	if BundledModels == nil {
		return false
	}
	_, err := fs.Stat(BundledModels, "models/"+slug+".toml")
	return err == nil
}

func LoadModel(path string) (*ModelConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening model config %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var m ModelConfig
	if _, err := toml.NewDecoder(f).Decode(&m); err != nil {
		return nil, fmt.Errorf("parsing model config %s: %w", path, err)
	}

	return &m, nil
}

func SaveModel(path string, m *ModelConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating model directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating model config %s: %w", path, err)
	}
	if err := toml.NewEncoder(f).Encode(m); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ModelConfigToBytes encodes m to TOML and returns the bytes.
func ModelConfigToBytes(m *ModelConfig) ([]byte, error) {
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func ListModels(dir string) ([]*ModelConfig, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading models dir %s: %w", dir, err)
	}

	var models []*ModelConfig
	var names []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}

		m, err := LoadModel(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, err
		}

		models = append(models, m)
		names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
	}

	return models, names, nil
}

// ListModelsFromDirs aggregates models from multiple directories, deduplicating
// by slug. Earlier dirs take precedence when the same slug appears in multiple dirs.
func ListModelsFromDirs(dirs ...string) ([]*ModelConfig, []string, error) {
	seen := make(map[string]bool)
	var models []*ModelConfig
	var names []string

	for _, dir := range dirs {
		cfgs, slugs, err := ListModels(dir)
		if err != nil {
			return nil, nil, err
		}
		for i, slug := range slugs {
			if seen[slug] {
				continue
			}
			seen[slug] = true
			names = append(names, slug)
			models = append(models, cfgs[i])
		}
	}
	return models, names, nil
}

// FindModelPath searches for a model TOML file in each directory in order and
// returns the first path found. Returns an error if the slug is not found in any dir.
func FindModelPath(slug string, dirs ...string) (string, error) {
	for _, dir := range dirs {
		p := filepath.Join(dir, slug+".toml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("model %q not found", slug)
}

// PersistModelHealthPath records healthPath as serve.health_path in the model's
// config file so future runs use it directly without probing. It is a no-op when
// the raw file already has that path set.
//
// If the file exists in a non-writable directory (e.g. a system models dir), the
// raw file is copied to the first writable directory with health_path added — that
// user-level file takes precedence via effectiveDirs ordering, and any extends chain
// in the raw file still resolves correctly because the parent slug is different.
func PersistModelHealthPath(slug, healthPath string, dirs ...string) error {
	// Find the raw (not merged) file and decode it.
	rawPath, err := FindModelPath(slug, dirs...)
	if err != nil {
		return nil // model not on disk (e.g. embedded only); skip
	}
	data, err := os.ReadFile(rawPath)
	if err != nil {
		return nil
	}
	var raw ModelConfig
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil
	}
	if raw.Serve.HealthPath == healthPath {
		return nil // already explicit and correct; nothing to do
	}

	raw.Serve.HealthPath = healthPath

	// Try to write to the file where it lives.
	if err := SaveModel(rawPath, &raw); err == nil {
		return nil
	}

	// rawPath not writable (likely a system dir). Copy to first writable dir.
	for _, dir := range dirs {
		dst := filepath.Join(dir, slug+".toml")
		if dst == rawPath {
			continue // already tried this one
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		if err := SaveModel(dst, &raw); err == nil {
			return nil
		}
	}
	return nil // no writable dir available; skip silently
}
