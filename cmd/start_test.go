package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
)

// noopEnableUnit disables enableUnit side-effects for the duration of the test.
func noopEnableUnit(t *testing.T) {
	t.Helper()
	old := enableUnit
	enableUnit = func(_ *config.Config) error { return nil }
	t.Cleanup(func() { enableUnit = old })
}

// TestStartNoActiveModel: no models → error propagates from switch.
func TestStartNoActiveModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	noopEnableUnit(t)

	_ = modelsDir
	_, err := executeCmd("start")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

// TestStartSingleModel: one model in dir, no arg → picker returns it directly, switch succeeds.
func TestStartSingleModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	out, err := executeCmd("start")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
}

// TestStartWithModelArg: explicit model arg → switches directly without picker.
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

// TestStartWithEnable: --enable calls enableUnit after a successful switch.
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

// TestStartEnableNoArg: single model + --enable → switches then calls enableUnit.
func TestStartEnableNoArg(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopRequireRoot(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	var enabled bool
	old := enableUnit
	enableUnit = func(_ *config.Config) error { enabled = true; return nil }
	t.Cleanup(func() { enableUnit = old })

	out, err := executeCmd("start", "--enable")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
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
	writeVLLMModel(t, modelsDir, "llama-8b")

	old := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return nil, fmt.Errorf("docker not available")
	}
	t.Cleanup(func() { buildProvider = old })

	_, err := executeCmd("start", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker not available")
}
