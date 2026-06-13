package cmd

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
)

// stubAdhoc is a controllable in-process stub for adhocRunner.
type stubAdhoc struct {
	startID         string
	startErr        error
	foregroundErr   error
	listResult      []provider.AdhocInfo
	listErr         error
	stopErr         error
	stopAllErr      error
	stopCalled      string
	stopAllCalled   bool
	unmanagedResult []provider.UnmanagedContainer
	unmanagedErr    error
}

func (s *stubAdhoc) Start(_ context.Context, _ string) (string, error) {
	return s.startID, s.startErr
}
func (s *stubAdhoc) RunForeground(_ context.Context, _ string, _ io.Writer) error {
	return s.foregroundErr
}
func (s *stubAdhoc) List(_ context.Context) ([]provider.AdhocInfo, error) {
	return s.listResult, s.listErr
}
func (s *stubAdhoc) Stop(_ context.Context, slug string) error {
	s.stopCalled = slug
	return s.stopErr
}
func (s *stubAdhoc) StopAll(_ context.Context) error {
	s.stopAllCalled = true
	return s.stopAllErr
}
func (s *stubAdhoc) LogsFor(_ context.Context, _ string, _ io.Writer, _ bool, _ int) error {
	return nil
}
func (s *stubAdhoc) DetectUnmanaged(_ context.Context) ([]provider.UnmanagedContainer, error) {
	return s.unmanagedResult, s.unmanagedErr
}

// injectAdhocRunner replaces buildAdhocRunner with one that always returns stub.
func injectAdhocRunner(t *testing.T, stub adhocRunner) {
	t.Helper()
	old := buildAdhocRunner
	buildAdhocRunner = func(_ *config.Config) (adhocRunner, error) { return stub, nil }
	t.Cleanup(func() { buildAdhocRunner = old })
}

func injectAdhocRunnerErr(t *testing.T, err error) {
	t.Helper()
	old := buildAdhocRunner
	buildAdhocRunner = func(_ *config.Config) (adhocRunner, error) { return nil, err }
	t.Cleanup(func() { buildAdhocRunner = old })
}

// --- marlin run ---

func TestRunDetachSuccess(t *testing.T) {
	stub := &stubAdhoc{startID: "abcdef123456789"}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t) // sets up temp config
	defer cleanup()

	out, err := executeCmd("run", "--detach", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "started llama-8b")
	assert.Contains(t, out, "abcdef123456") // first 12 chars
}

func TestRunDetachStartError(t *testing.T) {
	stub := &stubAdhoc{startErr: fmt.Errorf("pull failed")}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("run", "--detach", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pull failed")
}

func TestRunForegroundSuccess(t *testing.T) {
	stub := &stubAdhoc{foregroundErr: nil}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("run", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "running llama-8b")
}

func TestRunForegroundError(t *testing.T) {
	stub := &stubAdhoc{foregroundErr: fmt.Errorf("container exited")}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("run", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container exited")
}

func TestRunRunnerBuildError(t *testing.T) {
	injectAdhocRunnerErr(t, fmt.Errorf("docker unavailable"))
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("run", "--detach", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker unavailable")
}

// --- marlin ps ---

func TestPsEmpty(t *testing.T) {
	injectAdhocRunner(t, &stubAdhoc{})
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("ps")
	require.NoError(t, err)
	assert.Contains(t, out, "no marlin-managed containers")
}

func TestPsWithContainers(t *testing.T) {
	stub := &stubAdhoc{
		listResult: []provider.AdhocInfo{
			{Slug: "llama-8b", Provider: "vllm", Status: "running", Port: "8000", ID: "abc123456789xyz"},
		},
	}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("ps")
	require.NoError(t, err)
	assert.Contains(t, out, "llama-8b")
	assert.Contains(t, out, "vllm")
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "abc123456789") // truncated to 12
}

func TestPsListError(t *testing.T) {
	stub := &stubAdhoc{listErr: fmt.Errorf("docker down")}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("ps")
	assert.Error(t, err)
}

func TestPsRunnerBuildError(t *testing.T) {
	injectAdhocRunnerErr(t, fmt.Errorf("no docker"))
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("ps")
	assert.Error(t, err)
}

// --- marlin stop ---

func TestStopSlugSuccess(t *testing.T) {
	stub := &stubAdhoc{}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("stop", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "stopped llama-8b")
	assert.Equal(t, "llama-8b", stub.stopCalled)
}

func TestStopSlugError(t *testing.T) {
	stub := &stubAdhoc{stopErr: fmt.Errorf("not found")}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("stop", "llama-8b")
	assert.Error(t, err)
}

func TestStopAllSuccess(t *testing.T) {
	stub := &stubAdhoc{}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("stop")
	require.NoError(t, err)
	assert.Contains(t, out, "stopped all")
	assert.True(t, stub.stopAllCalled)
}

func TestStopAllError(t *testing.T) {
	stub := &stubAdhoc{stopAllErr: fmt.Errorf("docker down")}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("stop")
	assert.Error(t, err)
}

func TestStopRunnerBuildError(t *testing.T) {
	injectAdhocRunnerErr(t, fmt.Errorf("no docker"))
	_, cleanup := switchEnv(t)
	defer cleanup()

	_, err := executeCmd("stop")
	assert.Error(t, err)
}

// --- marlin status unmanaged warning ---

func TestStatusUnmanagedWarning(t *testing.T) {
	stub := &stubAdhoc{
		unmanagedResult: []provider.UnmanagedContainer{
			{ID: "aabbccddeeff1234", Image: "vllm/vllm-openai:latest", Names: []string{"/my-vllm"}},
		},
	}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("status")
	require.NoError(t, err)
	assert.Contains(t, out, "unmanaged inference containers detected")
	assert.Contains(t, out, "vllm/vllm-openai:latest")
	assert.Contains(t, out, "aabbccddeeff") // truncated ID
}

func TestStatusNoUnmanagedWarning(t *testing.T) {
	stub := &stubAdhoc{} // empty unmanagedResult
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()

	out, err := executeCmd("status")
	require.NoError(t, err)
	assert.NotContains(t, out, "unmanaged")
}
