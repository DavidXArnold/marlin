package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/DavidXArnold/marlin/internal/config"
)

// State tracks which model and provider are currently active on this machine.
type State struct {
	ActiveModel    string              `toml:"active_model"`    // slug (TOML filename without .toml)
	ActiveProvider config.ProviderType `toml:"active_provider"` // "vllm" or "nim"
	ContainerID    string              `toml:"container_id"`    // populated for nim, empty for vllm
}

func Empty() *State {
	return &State{}
}

func Load(path string) (*State, error) {
	s := Empty()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("opening state file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := toml.NewDecoder(f).Decode(s); err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", path, err)
	}

	return s, nil
}

func Save(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating state file %s: %w", path, err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(s); err != nil {
		return fmt.Errorf("writing state file %s: %w", path, err)
	}

	return nil
}
