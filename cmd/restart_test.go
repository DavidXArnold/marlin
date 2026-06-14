package cmd

import (
	"bytes"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
)

// restartCmdWithContext returns a *cobra.Command wired with the flags that
// runRestart (and runStart it calls) expects to find.
func restartCmdWithContext(buf io.Writer) *cobra.Command {
	cmd := cmdWithContext(buf)
	cmd.Flags().Bool("enable", false, "")
	cmd.Flags().BoolP("logs", "l", false, "")
	cmd.Flags().String("max-runtime", "", "")
	return cmd
}

// TestRestartNoArgNoActiveModel: no arg, no active model → runStart → no models → error.
func TestRestartNoArgNoActiveModel(t *testing.T) {
	_, cleanup := switchEnv(t)
	defer cleanup()

	err := runRestart(restartCmdWithContext(io.Discard), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

// TestRestartNoArgWithActiveModel: no arg, active model → stop then restart same model.
func TestRestartNoArgWithActiveModel(t *testing.T) {
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
	noopWaitForReady(t)

	var buf bytes.Buffer
	require.NoError(t, runRestart(restartCmdWithContext(&buf), nil))
	assert.True(t, p.stopCalled)
	out := buf.String()
	assert.Contains(t, out, "stopped llama-8b")
	assert.Contains(t, out, "switched to")
}

// TestRestartWithSameModelArg: arg == active model → stops then restarts.
func TestRestartWithSameModelArg(t *testing.T) {
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
	noopWaitForReady(t)

	var buf bytes.Buffer
	require.NoError(t, runRestart(restartCmdWithContext(&buf), []string{"llama-8b"}))
	assert.True(t, p.stopCalled)
	out := buf.String()
	assert.Contains(t, out, "stopped llama-8b")
	assert.Contains(t, out, "switched to")
}

// TestRestartWithDifferentModelArg: arg differs from active → stops active, starts named.
func TestRestartWithDifferentModelArg(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")
	writeVLLMModel(t, modelsDir, "qwen25-72b")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	p := &mockProv{}
	injectProvider(t, p)
	noopWaitForReady(t)

	var buf bytes.Buffer
	require.NoError(t, runRestart(restartCmdWithContext(&buf), []string{"qwen25-72b"}))
	assert.True(t, p.stopCalled)
	out := buf.String()
	assert.Contains(t, out, "stopped llama-8b")
	assert.Contains(t, out, "qwen25-72b")
}

// TestRestartWithArgNoCurrentActive: arg provided, no active model → skip stop, start named.
func TestRestartWithArgNoCurrentActive(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	p := &mockProv{}
	injectProvider(t, p)
	noopWaitForReady(t)

	var buf bytes.Buffer
	require.NoError(t, runRestart(restartCmdWithContext(&buf), []string{"llama-8b"}))
	assert.False(t, p.stopCalled)
	assert.Contains(t, buf.String(), "switched to")
}
