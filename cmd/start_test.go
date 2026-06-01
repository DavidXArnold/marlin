package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
)

// noopEnableUnit disables enableUnit side-effects for the duration of the test.
func noopEnableUnit(t *testing.T) {
	t.Helper()
	old := enableUnit
	enableUnit = func(_ *config.Config) error { return nil }
	t.Cleanup(func() { enableUnit = old })
}

// TestStartNoActiveModelOpensPicker: no active model → picker (resolveModel
// returns error when models dir is empty, which is fine to verify the path).
func TestStartNoActiveModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	noopEnableUnit(t)

	// No models → switch returns an error about empty models dir.
	_ = modelsDir
	_, err := executeCmd("start")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

// TestStartAlreadyRunning: active model + service running → no restart.
func TestStartAlreadyRunning(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))
	writeVLLMModel(t, modelsDir, "llama-8b")

	// Provider reports service running.
	injectProvider(t, &mockProv{statusRunning: true})

	out, err := executeCmd("start")
	require.NoError(t, err)
	assert.Contains(t, out, "already running")
}

// TestStartServiceStopped: active model + service stopped → switches to it.
func TestStartServiceStopped(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))
	writeVLLMModel(t, modelsDir, "llama-8b")

	// Default mockProv has statusRunning: false → service stopped.
	injectProvider(t, &mockProv{})

	out, err := executeCmd("start")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
}

// TestStartWithModelArg: explicit model arg → switches directly.
func TestStartWithModelArg(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	injectProvider(t, &mockProv{})

	writeVLLMModel(t, modelsDir, "llama-8b")

	out, err := executeCmd("start", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
}

// TestStartWithEnable: --enable calls enableUnit.
func TestStartWithEnable(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	var enabled bool
	old := enableUnit
	enableUnit = func(_ *config.Config) error { enabled = true; return nil }
	t.Cleanup(func() { enableUnit = old })

	_, err := executeCmd("start", "llama-8b", "--enable")
	require.NoError(t, err)
	assert.True(t, enabled)
}

// TestStartEnableAlreadyRunning: --enable still called even when already running.
func TestStartEnableAlreadyRunning(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))
	writeVLLMModel(t, modelsDir, "llama-8b")
	injectProvider(t, &mockProv{statusRunning: true})

	var enabled bool
	old := enableUnit
	enableUnit = func(_ *config.Config) error { enabled = true; return nil }
	t.Cleanup(func() { enableUnit = old })

	out, err := executeCmd("start", "--enable")
	require.NoError(t, err)
	assert.Contains(t, out, "already running")
	assert.True(t, enabled)
}

// TestStartEnableFails: enableUnit error propagates.
func TestStartEnableFails(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	old := enableUnit
	enableUnit = func(_ *config.Config) error { return fmt.Errorf("systemctl: permission denied") }
	t.Cleanup(func() { enableUnit = old })

	_, err := executeCmd("start", "llama-8b", "--enable")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

// TestStartProviderError: provider build failure is surfaced.
func TestStartProviderError(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	_ = modelsDir

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	old := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return nil, fmt.Errorf("docker not available")
	}
	t.Cleanup(func() { buildProvider = old })

	_, err = executeCmd("start")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker not available")
}
