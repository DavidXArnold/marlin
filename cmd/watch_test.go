package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/watchdog"
)

func TestWatchNoActiveModel(t *testing.T) {
	_, cleanup := switchEnv(t)
	defer cleanup()

	// Leave state file empty (no active model).
	var buf bytes.Buffer
	err := runWatch(cmdWithContext(&buf), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active model")
}

func TestWatchRunFuncInjectable(t *testing.T) {
	called := false
	old := watchRunFunc
	watchRunFunc = func(_ context.Context, _ watchdog.Config, _ func(context.Context) bool, slug string, _ func(context.Context) error, _ io.Writer) error {
		called = true
		assert.Equal(t, "myslug", slug)
		return nil
	}
	t.Cleanup(func() { watchRunFunc = old })

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "myslug")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "myslug",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectProvider(t, &mockProv{})

	var buf bytes.Buffer
	require.NoError(t, runWatch(cmdWithContext(&buf), nil))
	assert.True(t, called)
}

func TestWatchRunFuncError(t *testing.T) {
	old := watchRunFunc
	watchRunFunc = func(_ context.Context, _ watchdog.Config, _ func(context.Context) bool, _ string, _ func(context.Context) error, _ io.Writer) error {
		return errors.New("max restarts exceeded")
	}
	t.Cleanup(func() { watchRunFunc = old })

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "myslug")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "myslug",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectProvider(t, &mockProv{})

	err = runWatch(cmdWithContext(io.Discard), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max restarts exceeded")
}

func TestWatchExplicitSlug(t *testing.T) {
	var capturedSlug string
	old := watchRunFunc
	watchRunFunc = func(_ context.Context, _ watchdog.Config, _ func(context.Context) bool, slug string, _ func(context.Context) error, _ io.Writer) error {
		capturedSlug = slug
		return nil
	}
	t.Cleanup(func() { watchRunFunc = old })

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "active")

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "active",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectProvider(t, &mockProv{})

	require.NoError(t, runWatch(cmdWithContext(io.Discard), []string{"explicit-slug"}))
	assert.Equal(t, "explicit-slug", capturedSlug)
}
