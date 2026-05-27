//go:build integration

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationVLLMProviderSwitch verifies the filesystem side of a vLLM switch:
// env file written and symlink updated atomically, regardless of systemd outcome.
func TestIntegrationVLLMProviderSwitch(t *testing.T) {
	env := newTestEnv(t)
	slug := "qwen25-72b-awq"
	env.addModel(t, slug)

	p := provider.NewVLLMProvider(env.cfg)
	// systemd restart will fail in CI (no daemon / unit doesn't exist). That's
	// expected and acceptable — we only assert the filesystem work happened.
	_ = p.Switch(context.Background(), slug)

	envFile := filepath.Join(env.modelsDir, slug+".env")
	assert.FileExists(t, envFile, "env file must be written before the systemd call")

	target, err := os.Readlink(env.cfg.Paths.ActiveSymlink)
	require.NoError(t, err, "active symlink must exist after Switch")
	assert.Equal(t, envFile, target, "symlink must point at the model's env file")
}

// TestIntegrationVLLMProviderSwitchTwice verifies the symlink is replaced atomically
// on a second call (no stale .tmp symlink left behind).
func TestIntegrationVLLMProviderSwitchTwice(t *testing.T) {
	env := newTestEnv(t)
	env.addModel(t, "model-a")
	env.addModel(t, "model-b")

	p := provider.NewVLLMProvider(env.cfg)
	_ = p.Switch(context.Background(), "model-a")
	_ = p.Switch(context.Background(), "model-b")

	target, err := os.Readlink(env.cfg.Paths.ActiveSymlink)
	require.NoError(t, err)
	assert.Contains(t, target, "model-b.env", "symlink must point at second model after switch")

	// No leftover .tmp symlink.
	_, err = os.Lstat(env.cfg.Paths.ActiveSymlink + ".tmp")
	assert.True(t, os.IsNotExist(err), "tmp symlink must be cleaned up after rename")
}

func TestIntegrationVLLMProviderSwitchMissingModel(t *testing.T) {
	env := newTestEnv(t)
	p := provider.NewVLLMProvider(env.cfg)
	err := p.Switch(context.Background(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}
