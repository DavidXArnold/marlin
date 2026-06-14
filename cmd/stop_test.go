package cmd

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
)

// TestStopNoArgsNoActiveModel: no active model → falls through to StopAll on ad-hoc runner.
func TestStopNoArgsNoActiveModel(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	injectAdhocRunner(t, &stubAdhoc{})

	var buf bytes.Buffer
	require.NoError(t, runStop(cmdWithContext(&buf), nil))
	assert.Contains(t, buf.String(), "stopped all")
}

// TestStopNoArgsWithActiveModel: active model + no args → stopActiveModel, records stop time.
func TestStopNoArgsWithActiveModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	p := &mockProv{}
	injectProvider(t, p)

	var buf bytes.Buffer
	require.NoError(t, runStop(cmdWithContext(&buf), nil))
	assert.True(t, p.stopCalled)
	assert.Contains(t, buf.String(), "stopped llama-8b")

	// StoppedAt should be persisted.
	loaded, err := state.Load(cfg.Paths.StateFile)
	require.NoError(t, err)
	assert.NotNil(t, loaded.StoppedAt)
}

// TestStopWithSlugMatchingActiveModel: slug == active model → stops managed provider.
func TestStopWithSlugMatchingActiveModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	p := &mockProv{}
	injectProvider(t, p)

	var buf bytes.Buffer
	require.NoError(t, runStop(cmdWithContext(&buf), []string{"llama-8b"}))
	assert.True(t, p.stopCalled)
	assert.Contains(t, buf.String(), "stopped llama-8b")
}

// TestStopWithSlugAdHocContainer: slug != active model → delegated to ad-hoc runner.
func TestStopWithSlugAdHocContainer(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectAdhocRunner(t, &stubAdhoc{})

	var buf bytes.Buffer
	require.NoError(t, runStop(cmdWithContext(&buf), []string{"other-container"}))
	assert.Contains(t, buf.String(), "stopped other-container")
}

// TestStopBuildProviderError: buildProvider fails → error returned.
func TestStopBuildProviderError(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	old := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return nil, fmt.Errorf("docker unavailable")
	}
	t.Cleanup(func() { buildProvider = old })

	err = runStop(cmdWithContext(io.Discard), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker unavailable")
}

// TestStopProviderStopError: p.Stop() fails → error returned.
func TestStopProviderStopError(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectProvider(t, &mockProv{stopErr: fmt.Errorf("systemctl failed")})

	err = runStop(cmdWithContext(io.Discard), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemctl failed")
}

// TestStopAdhocRunnerBuildError: buildAdhocRunner fails → error propagated.
func TestStopAdhocRunnerBuildError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	injectAdhocRunnerErr(t, fmt.Errorf("no docker"))

	err := runStop(cmdWithContext(io.Discard), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no docker")
}
