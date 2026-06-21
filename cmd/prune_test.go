package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneDryRun(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	// Create a fake NIM cache entry.
	nimDir := filepath.Join(cfg.Paths.NIMCache, "llama-3-8b")
	require.NoError(t, os.MkdirAll(nimDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nimDir, "model.gguf"), make([]byte, 1024), 0o644))

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("apply", false, "")
	cmd.Flags().String("hf-cache", "/nonexistent-hf-cache", "")
	require.NoError(t, runPrune(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "DRY RUN")
	assert.Contains(t, out, "nim-cache")
	assert.Contains(t, out, "--apply")
}

func TestPruneApply(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	nimDir := filepath.Join(cfg.Paths.NIMCache, "to-delete")
	require.NoError(t, os.MkdirAll(nimDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nimDir, "data"), []byte("x"), 0o644))

	var deleted []string
	old := pruneRemoveAll
	pruneRemoveAll = func(path string) error {
		deleted = append(deleted, path)
		return os.RemoveAll(path)
	}
	defer func() { pruneRemoveAll = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("apply", false, "")
	cmd.Flags().String("hf-cache", "/nonexistent-hf-cache", "")
	require.NoError(t, cmd.Flags().Set("apply", "true"))
	require.NoError(t, runPrune(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "APPLY")
	assert.Contains(t, out, "deleted")
	assert.Len(t, deleted, 1)
	assert.Contains(t, deleted[0], "to-delete")
}

func TestPruneNothing(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("apply", false, "")
	cmd.Flags().String("hf-cache", "/nonexistent-hf-cache", "")
	require.NoError(t, runPrune(cmd, nil))

	assert.Contains(t, buf.String(), "nothing to prune")
}

func TestPruneHFCache(t *testing.T) {
	dir := t.TempDir()
	modelDir := filepath.Join(dir, "models--org--llama")
	require.NoError(t, os.MkdirAll(modelDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modelDir, "config.json"), []byte("{}"), 0o644))

	entries, err := scanHFCache(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "hf-cache", entries[0].label)
	assert.Contains(t, entries[0].path, "models--org--llama")
}

func TestPruneDirSizeGB(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), make([]byte, 1024*1024), 0o644))
	size := dirSizeGB(dir)
	assert.Greater(t, size, 0.0)
	assert.Less(t, size, 1.0) // 1 MiB << 1 GiB
}
