package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
)

// --- lastNonEmptyLine ---

func TestLastNonEmptyLine(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"\n\n", ""},
		{"only line", "only line"},
		{"first\nsecond\nthird", "third"},
		{"first\nsecond\n\n", "second"},
		{"  spaces  \n  trimmed  \n", "trimmed"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, lastNonEmptyLine(tc.input), "input %q", tc.input)
	}
}

// --- runStatus NIM provider paths ---

// fakeNIMProvider is a minimal provider.Provider that returns a fixed Status and
// streams a canned log line.
type fakeNIMProvider struct {
	status       *provider.Status
	statusErr    error
	logLine      string
	logsErr      error
}

func (f *fakeNIMProvider) Switch(_ context.Context, _ string) error             { return nil }
func (f *fakeNIMProvider) Stop(_ context.Context) error                         { return nil }
func (f *fakeNIMProvider) Status(_ context.Context) (*provider.Status, error) {
	return f.status, f.statusErr
}
func (f *fakeNIMProvider) Logs(_ context.Context, w io.Writer, _ bool, _ int) error {
	if f.logsErr != nil {
		return f.logsErr
	}
	if f.logLine != "" {
		_, _ = fmt.Fprintln(w, f.logLine)
	}
	return nil
}

// nimStatusEnv sets up a temp env with a NIM model active.
func nimStatusEnv(t *testing.T) (containerID string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "nim-model.toml"), []byte(`[model]
image = "nvcr.io/nim/meta/llama:latest"
type = "nim"
status = "untested"
`), 0o644))

	cfgContent := fmt.Sprintf(`[behavior]
warn_unmanaged_containers = false

[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q
nim_cache = %q

[server]
host = "127.0.0.1"
port = 19999
alias = "test"
`, modelsDir,
		filepath.Join(dir, "state.toml"),
		filepath.Join(dir, "secrets.env"),
		filepath.Join(dir, "model.env"),
		filepath.Join(dir, "nim-cache"),
	)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))

	containerID = "abc1234567890def"
	require.NoError(t, state.Save(filepath.Join(dir, "state.toml"), &state.State{
		ActiveModel:    "nim-model",
		ActiveProvider: config.ProviderNIM,
		ContainerID:    containerID,
	}))

	old := cfgFile
	cfgFile = cfgPath
	return containerID, func() { cfgFile = old }
}

func TestRunStatusNIMContainerRunning(t *testing.T) {
	_, cleanup := nimStatusEnv(t)
	defer cleanup()

	fakeProvider := &fakeNIMProvider{
		status: &provider.Status{
			Running:        true,
			ContainerID:    "deadbeef000011112222",
			ContainerState: "running",
		},
	}
	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return fakeProvider, nil
	}
	defer func() { buildProvider = oldBuild }()

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "container    : deadbeef0000  (running)")
	assert.Contains(t, out, "api health   : not ready")
}

func TestRunStatusNIMContainerExited(t *testing.T) {
	_, cleanup := nimStatusEnv(t)
	defer cleanup()

	fakeProvider := &fakeNIMProvider{
		status: &provider.Status{
			Running:        false,
			ContainerID:    "cafebabe11223344",
			ContainerState: "exited",
		},
		logLine: "FATAL: GPU not found",
	}
	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return fakeProvider, nil
	}
	defer func() { buildProvider = oldBuild }()

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "container    : cafebabe1122  (exited)")
	assert.Contains(t, out, "api health   : not ready")
	assert.Contains(t, out, "last log     : FATAL: GPU not found")
}

func TestRunStatusNIMContainerNotFound(t *testing.T) {
	_, cleanup := nimStatusEnv(t)
	defer cleanup()

	fakeProvider := &fakeNIMProvider{
		status: &provider.Status{
			Running:        false,
			ContainerState: "not found",
		},
	}
	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return fakeProvider, nil
	}
	defer func() { buildProvider = oldBuild }()

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "(not found)")
}

func TestRunStatusNIMBuildProviderError(t *testing.T) {
	cid, cleanup := nimStatusEnv(t)
	defer cleanup()

	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return nil, fmt.Errorf("docker not available")
	}
	defer func() { buildProvider = oldBuild }()

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	// Falls back to state ContainerID when live query fails.
	assert.Contains(t, out, cid[:12])
}
