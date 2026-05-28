package provider

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

// stubRunner returns a cmdOutput func that records calls and returns preset outputs.
type stubRunner struct {
	calls  [][]string
	output []byte
	err    error
}

func (s *stubRunner) run(_ context.Context, args ...string) ([]byte, error) {
	s.calls = append(s.calls, args)
	return s.output, s.err
}

func newContainerdProviderWithStub(t *testing.T, stub *stubRunner) *ContainerdNIMProvider {
	t.Helper()
	cfg := config.Defaults()
	cfg.Paths.ModelsDir = t.TempDir()
	cfg.Paths.NIMCache = t.TempDir()
	return newContainerdNIMProviderWithRunner(cfg, "test-key", stub.run)
}

func writeNIMModelFixture(t *testing.T, modelsDir, slug string) {
	t.Helper()
	mc := &config.ModelConfig{
		Model: config.ModelMeta{
			Type:  config.ProviderNIM,
			ID:    "llama-3.1-8b-instruct",
			Image: "nvcr.io/nim/meta/llama-3.1-8b-instruct:latest",
		},
		Serve: config.ServeConfig{
			GPUMemoryUtilization: 0.9,
		},
	}
	require.NoError(t, config.SaveModel(filepath.Join(modelsDir, slug+".toml"), mc))
}

// — NewContainerdNIMProvider —

func TestNewContainerdNIMProviderNoNerdctl(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH → nerdctl lookup fails
	cfg := config.Defaults()
	_, err := NewContainerdNIMProvider(cfg, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nerdctl")
}

// — Switch —

func TestContainerdNIMProviderSwitchMissingModel(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)

	err := p.Switch(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestContainerdNIMProviderSwitchNoImage(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)

	// Write a model config with no image field.
	mc := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderNIM, ID: "test"},
	}
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "no-image.toml"), mc))

	err := p.Switch(context.Background(), "no-image")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no image")
}

func TestContainerdNIMProviderSwitchPullError(t *testing.T) {
	stub := &stubRunner{output: []byte("pull failed"), err: fmt.Errorf("exit 1")}
	p := newContainerdProviderWithStub(t, stub)
	p.ngcKey = "" // skip login branch

	writeNIMModelFixture(t, p.cfg.Paths.ModelsDir, "llama-8b")
	err := p.Switch(context.Background(), "llama-8b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pulling image")
}

func TestContainerdNIMProviderSwitchRunError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Paths.ModelsDir = t.TempDir()
	cfg.Paths.NIMCache = t.TempDir()

	// pull/stop/rm succeed; run fails
	p := newContainerdNIMProviderWithRunner(cfg, "", func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "run" {
			return []byte("run failed"), fmt.Errorf("exit 125")
		}
		return nil, nil
	})

	writeNIMModelFixture(t, p.cfg.Paths.ModelsDir, "llama-8b")
	err := p.Switch(context.Background(), "llama-8b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starting NIM container")
}

func TestContainerdNIMProviderSwitchSuccess(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)
	p.ngcKey = "" // skip login branch

	writeNIMModelFixture(t, p.cfg.Paths.ModelsDir, "llama-8b")
	err := p.Switch(context.Background(), "llama-8b")
	require.NoError(t, err)

	// pull + stop + rm + run = 4 calls minimum
	assert.GreaterOrEqual(t, len(stub.calls), 2)
	assert.Equal(t, "pull", stub.calls[0][0])
}

func TestContainerdNIMProviderSwitchWithLogin(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)
	p.ngcKey = "ngc-test-key"

	var loginRegistry, loginKey string
	p.loginFunc = func(_ context.Context, registry, key string) error {
		loginRegistry = registry
		loginKey = key
		return nil
	}

	writeNIMModelFixture(t, p.cfg.Paths.ModelsDir, "llama-8b")
	err := p.Switch(context.Background(), "llama-8b")
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io", loginRegistry)
	assert.Equal(t, "ngc-test-key", loginKey)
}

func TestContainerdNIMProviderSwitchLoginError(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)
	p.ngcKey = "bad-key"
	p.loginFunc = func(_ context.Context, _, _ string) error {
		return fmt.Errorf("unauthorized")
	}

	writeNIMModelFixture(t, p.cfg.Paths.ModelsDir, "llama-8b")
	err := p.Switch(context.Background(), "llama-8b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
}

// — Stop —

func TestContainerdNIMProviderStop(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)
	assert.NoError(t, p.Stop(context.Background()))
	// stop + rm = 2 calls
	require.Len(t, stub.calls, 2)
	assert.Equal(t, "stop", stub.calls[0][0])
	assert.Equal(t, "rm", stub.calls[1][0])
}

// — Status —

func TestContainerdNIMProviderStatusNotFound(t *testing.T) {
	stub := &stubRunner{err: fmt.Errorf("exit 1")}
	p := newContainerdProviderWithStub(t, stub)

	status, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Running)
}

func TestContainerdNIMProviderStatusRunning(t *testing.T) {
	inspectJSON := []byte(`[{"Id":"abc123","Image":"nvcr.io/nim/meta/llama-3.1-8b-instruct:latest","State":{"Status":"running"}}]`)
	stub := &stubRunner{output: inspectJSON}
	p := newContainerdProviderWithStub(t, stub)

	status, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Equal(t, "abc123", status.ContainerID)
	assert.Equal(t, "llama-3.1-8b-instruct", status.ModelID)
}

func TestContainerdNIMProviderStatusStopped(t *testing.T) {
	inspectJSON := []byte(`[{"Id":"abc123","Image":"nvcr.io/nim/meta/llama-3.1-8b-instruct:latest","State":{"Status":"exited"}}]`)
	stub := &stubRunner{output: inspectJSON}
	p := newContainerdProviderWithStub(t, stub)

	status, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Running)
	assert.Equal(t, "abc123", status.ContainerID)
}

func TestContainerdNIMProviderStatusBadJSON(t *testing.T) {
	stub := &stubRunner{output: []byte("not json")}
	p := newContainerdProviderWithStub(t, stub)

	status, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Running)
}

func TestContainerdNIMProviderStatusEmptyArray(t *testing.T) {
	stub := &stubRunner{output: []byte("[]")}
	p := newContainerdProviderWithStub(t, stub)

	status, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Running)
}

func TestContainerdNIMProviderLogs(t *testing.T) {
	stub := &stubRunner{}
	p := newContainerdProviderWithStub(t, stub)

	var gotName string
	var gotArgs []string
	restore := SetRunCommandForTest(func(_ context.Context, w io.Writer, name string, args ...string) error {
		gotName = name
		gotArgs = args
		_, _ = fmt.Fprint(w, "log line")
		return nil
	})
	defer restore()

	var buf strings.Builder
	require.NoError(t, p.Logs(context.Background(), &buf, true, 25))
	assert.Equal(t, "nerdctl", gotName)
	assert.Contains(t, gotArgs, "logs")
	assert.Contains(t, gotArgs, "-f")
	assert.Contains(t, gotArgs, "25")
	assert.Contains(t, buf.String(), "log line")
}

// — defaultPodmanSocket —

func TestDefaultPodmanSocket(t *testing.T) {
	got := defaultPodmanSocket()
	assert.NotEmpty(t, got)
	assert.Contains(t, got, "podman.sock")
}

// — nerdctl live tests (skipped when nerdctl not on PATH) —

func TestContainerdNIMProviderLiveStatusNoContainer(t *testing.T) {
	if _, err := exec.LookPath("nerdctl"); err != nil {
		t.Skip("nerdctl not on PATH")
	}
	cfg := config.Defaults()
	p, err := NewContainerdNIMProvider(cfg, "")
	require.NoError(t, err)

	status, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Running)
}
