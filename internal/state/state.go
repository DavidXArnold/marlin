package state

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
)

// State tracks which model and provider are currently active on this machine.
type State struct {
	ActiveModel    string               `toml:"active_model"`    // slug (TOML filename without .toml)
	ActiveProvider config.ProviderType  `toml:"active_provider"` // "vllm" or "nim"
	ContainerID    string               `toml:"container_id"`    // populated for nim, empty for vllm
	PinnedDigest   string               `toml:"pinned_digest"`   // OCI digest of the running NIM image (sha256:...)
	ModelHistory   map[string]time.Time `toml:"model_history"`   // slug → last started time
	StoppedAt      *time.Time           `toml:"stopped_at"`      // set when stopped via marlin stop; nil when running
}

func Empty() *State {
	return &State{
		ModelHistory: make(map[string]time.Time),
	}
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
	defer func() { _ = f.Close() }()

	if _, err := toml.NewDecoder(f).Decode(s); err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", path, err)
	}

	if s.ModelHistory == nil {
		s.ModelHistory = make(map[string]time.Time)
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

	if err := toml.NewEncoder(f).Encode(s); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing state file %s: %w", path, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("closing state file %s: %w", path, err)
	}

	return nil
}

// SavePrivileged encodes state and writes it to path. If the directory requires
// elevated privileges it prompts w for confirmation then writes via sudo tee.
func SavePrivileged(w io.Writer, path string, s *State) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}
	ok, err := privilege.PromptAndWriteFile(w, filepath.Dir(path), path, buf.Bytes())
	if err != nil {
		return fmt.Errorf("writing state file %s: %w", path, err)
	}
	if !ok {
		return fmt.Errorf("cancelled")
	}
	return nil
}

// RecordStart updates the history map with the current time for slug and
// clears any pending StoppedAt marker.
func RecordStart(s *State, slug string) {
	if s.ModelHistory == nil {
		s.ModelHistory = make(map[string]time.Time)
	}
	s.ModelHistory[slug] = time.Now()
	s.StoppedAt = nil
}

// RecordStop marks the state as manually stopped at the current time.
func RecordStop(s *State) {
	t := time.Now()
	s.StoppedAt = &t
}
