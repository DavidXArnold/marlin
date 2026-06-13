package cmd

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
)

// logsAdhocRunner is a test double for adhocRunner used in logs tests.
type logsAdhocRunner struct {
	containers []provider.AdhocInfo
	logsErr    error
}

func (r *logsAdhocRunner) Start(_ context.Context, _ string) (string, error)   { return "", nil }
func (r *logsAdhocRunner) RunForeground(ctx context.Context, _ string, _ io.Writer) error {
	<-ctx.Done(); return nil
}
func (r *logsAdhocRunner) LogsFor(_ context.Context, _ string, _ io.Writer, _ bool, _ int) error {
	return r.logsErr
}
func (r *logsAdhocRunner) List(_ context.Context) ([]provider.AdhocInfo, error) {
	return r.containers, nil
}
func (r *logsAdhocRunner) Stop(_ context.Context, _ string) error               { return nil }
func (r *logsAdhocRunner) StopAll(_ context.Context) error                      { return nil }
func (r *logsAdhocRunner) DetectUnmanaged(_ context.Context) ([]provider.UnmanagedContainer, error) {
	return nil, nil
}

func testCmd() *cobra.Command {
	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	return cmd
}

// TestResolveLogsNoRunningModels: no adhoc, no active model → error.
func TestResolveLogsNoRunningModels(t *testing.T) {
	runner := &logsAdhocRunner{}
	cur := &state.State{}
	_, err := resolveLogsTarget("", cur, runner, testCmd())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running models")
}

// TestResolveLogsNilRunnerNoActiveModel: nil runner, no active model → error (no docker, no managed).
func TestResolveLogsNilRunnerNoActiveModel(t *testing.T) {
	cur := &state.State{}
	_, err := resolveLogsTarget("", cur, nil, testCmd())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running models")
}

// TestResolveLogsNilRunnerWithActiveModel: nil runner, active managed model → managed path.
func TestResolveLogsNilRunnerWithActiveModel(t *testing.T) {
	cur := &state.State{ActiveModel: "llama-8b", ActiveProvider: "vllm"}
	target, err := resolveLogsTarget("", cur, nil, testCmd())
	require.NoError(t, err)
	assert.False(t, target.useAdhoc)
}

// TestResolveLogsActiveManagedOnly: no adhoc → uses active managed model.
func TestResolveLogsActiveManagedOnly(t *testing.T) {
	runner := &logsAdhocRunner{}
	cur := &state.State{ActiveModel: "llama-8b", ActiveProvider: "vllm"}
	target, err := resolveLogsTarget("", cur, runner, testCmd())
	require.NoError(t, err)
	assert.False(t, target.useAdhoc)
}

// TestResolveLogsSingleAdhoc: one running adhoc → use it directly (no picker).
func TestResolveLogsSingleAdhoc(t *testing.T) {
	runner := &logsAdhocRunner{containers: []provider.AdhocInfo{
		{Slug: "qwen-7b", Status: "running", Port: "8001"},
	}}
	cur := &state.State{}
	target, err := resolveLogsTarget("", cur, runner, testCmd())
	require.NoError(t, err)
	assert.True(t, target.useAdhoc)
	assert.Equal(t, "qwen-7b", target.slug)
}

// TestResolveLogsExplicitAdhocName: query matches running adhoc → use it.
func TestResolveLogsExplicitAdhocName(t *testing.T) {
	runner := &logsAdhocRunner{containers: []provider.AdhocInfo{
		{Slug: "qwen-7b", Status: "running"},
	}}
	cur := &state.State{ActiveModel: "llama-8b", ActiveProvider: "vllm"}
	target, err := resolveLogsTarget("qwen-7b", cur, runner, testCmd())
	require.NoError(t, err)
	assert.True(t, target.useAdhoc)
	assert.Equal(t, "qwen-7b", target.slug)
}

// TestResolveLogsExplicitNameNoAdhocMatch: query does not match any adhoc → managed path.
func TestResolveLogsExplicitNameNoAdhocMatch(t *testing.T) {
	runner := &logsAdhocRunner{}
	cur := &state.State{ActiveModel: "llama-8b", ActiveProvider: "vllm"}
	target, err := resolveLogsTarget("llama-8b", cur, runner, testCmd())
	require.NoError(t, err)
	assert.False(t, target.useAdhoc)
}

// TestResolveLogsStoppedFallback: nothing running but a stopped container exists → use it.
func TestResolveLogsStoppedFallback(t *testing.T) {
	runner := &logsAdhocRunner{containers: []provider.AdhocInfo{
		{Slug: "old-run", Status: "exited"},
	}}
	cur := &state.State{}
	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	target, err := resolveLogsTarget("", cur, runner, cmd)
	require.NoError(t, err)
	assert.True(t, target.useAdhoc)
	assert.Equal(t, "old-run", target.slug)
}

// TestRunLogsWithArgQuery: when args has a model name, query is passed to resolver.
func TestRunLogsWithArgQuery(t *testing.T) {
	restore := provider.SetRunCommandForTest(func(_ context.Context, _ io.Writer, _ string, _ ...string) error {
		return nil
	})
	defer restore()
	injectManagedLogsTarget(t)

	cleanup := tempEnv(t)
	defer cleanup()

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	require.NoError(t, runLogs(cmd, []string{"some-model"}))
}

// TestRunLogsResolveError: resolveLogsTargetFunc returning an error propagates up.
func TestRunLogsResolveError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := resolveLogsTargetFunc
	resolveLogsTargetFunc = func(_ string, _ *state.State, _ adhocRunner, _ *cobra.Command) (logsTarget, error) {
		return logsTarget{}, fmt.Errorf("resolution failed")
	}
	defer func() { resolveLogsTargetFunc = old }()

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	err := runLogs(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resolution failed")
}

// TestRunLogsAdhocPath: when resolveLogsTarget returns useAdhoc=true, runner.LogsFor is called.
func TestRunLogsAdhocPath(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := resolveLogsTargetFunc
	resolveLogsTargetFunc = func(_ string, _ *state.State, _ adhocRunner, _ *cobra.Command) (logsTarget, error) {
		return logsTarget{useAdhoc: true, slug: "qwen-7b"}, nil
	}
	defer func() { resolveLogsTargetFunc = old }()

	injectAdhocRunner(t, &stubAdhoc{})

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	require.NoError(t, runLogs(cmd, nil))
}

// TestRunLogsAdhocDockerUnavailable: useAdhoc=true but runner nil → error.
func TestRunLogsAdhocDockerUnavailable(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := resolveLogsTargetFunc
	resolveLogsTargetFunc = func(_ string, _ *state.State, _ adhocRunner, _ *cobra.Command) (logsTarget, error) {
		return logsTarget{useAdhoc: true, slug: "qwen-7b"}, nil
	}
	defer func() { resolveLogsTargetFunc = old }()

	oldBuild := buildAdhocRunner
	buildAdhocRunner = func(_ *config.Config) (adhocRunner, error) {
		return nil, fmt.Errorf("docker not available")
	}
	defer func() { buildAdhocRunner = oldBuild }()

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	err := runLogs(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker is not available")
}
