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
