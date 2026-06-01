package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)


type ModelStatus string
type ProviderType string

const (
	StatusWorking  ModelStatus = "working"
	StatusBroken   ModelStatus = "broken"
	StatusUntested ModelStatus = "untested"

	ProviderVLLM ProviderType = "vllm"
	ProviderNIM  ProviderType = "nim"
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
}

type ServeConfig struct {
	Quantization         string   `toml:"quantization"`
	ToolCallParser       string   `toml:"tool_call_parser"`
	ServedModelName      []string `toml:"served_model_name"`
	GPUMemoryUtilization float64  `toml:"gpu_memory_utilization"`
	MaxModelLen          int      `toml:"max_model_len"`
	ExtraFlags           []string `toml:"extra_flags"`
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
