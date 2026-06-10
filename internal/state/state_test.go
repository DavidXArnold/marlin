package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmpty(t *testing.T) {
	s := Empty()
	assert.Equal(t, "", s.ActiveModel)
	assert.Equal(t, config.ProviderType(""), s.ActiveProvider)
	assert.Equal(t, "", s.ContainerID)
}

func TestLoadMissing(t *testing.T) {
	s, err := Load("/nonexistent/path/state.toml")
	require.NoError(t, err)
	assert.Equal(t, "", s.ActiveModel)
}

func TestLoadInvalidTOML(t *testing.T) {
	f, err := os.CreateTemp("", "state-*.toml")
	require.NoError(t, err)
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = f.WriteString("[[[[not valid")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = Load(f.Name())
	assert.Error(t, err)
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marlin", "state.toml")

	s := &State{
		ActiveModel:    "qwen25-72b",
		ActiveProvider: config.ProviderVLLM,
		ContainerID:    "",
	}

	require.NoError(t, Save(path, s))

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "qwen25-72b", loaded.ActiveModel)
	assert.Equal(t, config.ProviderVLLM, loaded.ActiveProvider)
	assert.Equal(t, "", loaded.ContainerID)
}

func TestSaveNIM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.toml")

	s := &State{
		ActiveModel:    "llama-3.1-8b-nim",
		ActiveProvider: config.ProviderNIM,
		ContainerID:    "abc123def456",
	}

	require.NoError(t, Save(path, s))

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, config.ProviderNIM, loaded.ActiveProvider)
	assert.Equal(t, "abc123def456", loaded.ContainerID)
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	// Nested path that doesn't exist yet
	path := filepath.Join(dir, "a", "b", "c", "state.toml")

	s := &State{ActiveModel: "test", ActiveProvider: config.ProviderVLLM}
	require.NoError(t, Save(path, s))

	_, err := os.Stat(path)
	assert.NoError(t, err)
}

func TestSaveUnwritable(t *testing.T) {
	s := &State{ActiveModel: "test"}
	err := Save("/proc/marlin/state.toml", s)
	assert.Error(t, err)
}

func TestSaveCannotCreate(t *testing.T) {
	dir := t.TempDir()
	// Make state.toml a directory so os.Create fails.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "state.toml"), 0755))
	err := Save(filepath.Join(dir, "state.toml"), &State{ActiveModel: "test"})
	assert.Error(t, err)
}

func TestRecordStart(t *testing.T) {
	s := Empty()
	before := time.Now()
	RecordStart(s, "qwen25-72b")
	assert.WithinDuration(t, before, s.ModelHistory["qwen25-72b"], time.Second)
}

func TestRecordStartNilMap(t *testing.T) {
	s := &State{} // ModelHistory is nil
	RecordStart(s, "llama-8b")
	assert.NotNil(t, s.ModelHistory)
	assert.False(t, s.ModelHistory["llama-8b"].IsZero())
}

func TestSaveAndLoadWithHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.toml")

	ts := time.Now().Truncate(time.Second)
	s := &State{
		ActiveModel:    "qwen25-72b",
		ActiveProvider: config.ProviderVLLM,
		ModelHistory:   map[string]time.Time{"qwen25-72b": ts},
	}
	require.NoError(t, Save(path, s))

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.WithinDuration(t, ts, loaded.ModelHistory["qwen25-72b"], time.Second)
}

func TestLoadEnsuresModelHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.toml")
	// Write state file without model_history key.
	require.NoError(t, os.WriteFile(path, []byte("active_model = \"test\"\n"), 0644))
	s, err := Load(path)
	require.NoError(t, err)
	assert.NotNil(t, s.ModelHistory)
}

func TestLoadPermissionDenied(t *testing.T) {
	f, err := os.CreateTemp("", "state-*.toml")
	require.NoError(t, err)
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = f.WriteString("active_model = \"test\"\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Chmod(f.Name(), 0000))
	defer func() { _ = os.Chmod(f.Name(), 0644) }()

	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	_, err = Load(f.Name())
	assert.Error(t, err)
}
