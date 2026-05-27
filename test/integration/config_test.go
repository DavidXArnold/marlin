//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationConfigLoad(t *testing.T) {
	env := newTestEnv(t)
	cfg, err := config.Load(env.cfgPath)
	require.NoError(t, err)
	assert.Equal(t, env.modelsDir, cfg.Paths.ModelsDir)
	assert.Equal(t, env.cfg.Paths.ActiveSymlink, cfg.Paths.ActiveSymlink)
	assert.Equal(t, "marlin-nonexistent-test-unit", cfg.Service.SystemdUnit)
}

func TestIntegrationConfigLoadMissing(t *testing.T) {
	cfg, err := config.Load("/nonexistent/path/config.toml")
	require.NoError(t, err, "missing config should fall back to defaults, not error")
	assert.Equal(t, config.Defaults().Paths.ModelsDir, cfg.Paths.ModelsDir)
}

func TestIntegrationModelRoundtrip(t *testing.T) {
	env := newTestEnv(t)
	env.addModel(t, "roundtrip-model")

	m, err := config.LoadModel(filepath.Join(env.modelsDir, "roundtrip-model.toml"))
	require.NoError(t, err)
	assert.Equal(t, config.ProviderVLLM, m.Model.Type)
	assert.Equal(t, "Qwen/Qwen2.5-72B-Instruct-AWQ", m.Model.ID)
	assert.InDelta(t, 0.92, m.Serve.GPUMemoryUtilization, 0.001)
	assert.Equal(t, config.StatusWorking, m.Model.Status)

	// Save to a new path and reload — encoding must be symmetric.
	copyPath := filepath.Join(t.TempDir(), "copy.toml")
	require.NoError(t, config.SaveModel(copyPath, m))

	m2, err := config.LoadModel(copyPath)
	require.NoError(t, err)
	assert.Equal(t, m.Model.ID, m2.Model.ID)
	assert.Equal(t, m.Serve.GPUMemoryUtilization, m2.Serve.GPUMemoryUtilization)
	assert.Equal(t, m.Serve.ServedModelName, m2.Serve.ServedModelName)
}

func TestIntegrationListModels(t *testing.T) {
	env := newTestEnv(t)
	env.addModel(t, "model-alpha")
	env.addModel(t, "model-beta")

	models, names, err := config.ListModels(env.modelsDir)
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "model-alpha")
	assert.Contains(t, names, "model-beta")
}

func TestIntegrationListModelsEmpty(t *testing.T) {
	env := newTestEnv(t)
	models, names, err := config.ListModels(env.modelsDir)
	require.NoError(t, err)
	assert.Empty(t, models)
	assert.Empty(t, names)
}

func TestIntegrationStateRoundtrip(t *testing.T) {
	// Use a nested subdir to exercise the MkdirAll path in state.Save.
	statePath := filepath.Join(t.TempDir(), "sub", "state.toml")

	original := &state.State{
		ActiveModel:    "qwen25-72b-awq",
		ActiveProvider: config.ProviderVLLM,
		ContainerID:    "",
	}
	require.NoError(t, state.Save(statePath, original))

	_, err := os.Stat(statePath)
	require.NoError(t, err, "state file should exist after Save")

	loaded, err := state.Load(statePath)
	require.NoError(t, err)
	assert.Equal(t, original.ActiveModel, loaded.ActiveModel)
	assert.Equal(t, original.ActiveProvider, loaded.ActiveProvider)
}

func TestIntegrationStateLoadMissing(t *testing.T) {
	s, err := state.Load("/nonexistent/path/state.toml")
	require.NoError(t, err, "missing state file should return empty state, not error")
	assert.Empty(t, s.ActiveModel)
	assert.Empty(t, s.ActiveProvider)
}
