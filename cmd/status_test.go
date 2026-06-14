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
	"github.com/DavidXArnold/marlin/internal/sysinfo"
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

// --- lastNLines ---

func TestLastNLines(t *testing.T) {
	assert.Empty(t, lastNLines("", 2))
	assert.Equal(t, []string{"only"}, lastNLines("only", 2))
	assert.Equal(t, []string{"first", "second"}, lastNLines("first\nsecond", 2))
	assert.Equal(t, []string{"second", "third"}, lastNLines("first\nsecond\nthird", 2))
	// trailing newlines are ignored
	assert.Equal(t, []string{"a", "b"}, lastNLines("a\nb\n\n", 3))
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
	assert.Contains(t, out, "not found")
}

// --- nimHint ---

func TestNimHint(t *testing.T) {
	cases := []struct {
		logs string
		want string
	}{
		{"", ""},
		{"normal startup log", ""},
		{"UMA device detected: using unified memory", "UMA"},
		{"No available memory for model", "UMA"},
		{"CUDA out of memory", "OOM"},
		{"RuntimeError: out of memory", "OOM"},
	}
	for _, tc := range cases {
		hint := nimHint(tc.logs)
		if tc.want == "" {
			assert.Empty(t, hint, "logs: %q", tc.logs)
		} else {
			assert.NotEmpty(t, hint, "logs: %q", tc.logs)
		}
	}
}

func TestRunStatusNIMShowsHintOnUMALog(t *testing.T) {
	_, cleanup := nimStatusEnv(t)
	defer cleanup()

	fakeProvider := &fakeNIMProvider{
		status: &provider.Status{
			Running:        false,
			ContainerState: "exited",
		},
		logLine: "UMA device detected: using unified memory",
	}
	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return fakeProvider, nil
	}
	defer func() { buildProvider = oldBuild }()

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "hint")
	assert.Contains(t, out, "NIM_PASSTHROUGH_ARGS")
}

func TestRunStatusUMAHintShownForNIM(t *testing.T) {
	_, cleanup := nimStatusEnv(t)
	defer cleanup()

	fakeProvider := &fakeNIMProvider{
		status: &provider.Status{Running: false, ContainerState: "not found"},
	}
	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return fakeProvider, nil
	}
	defer func() { buildProvider = oldBuild }()

	// Inject a GB10 (UMA) GPU via the sysinfo mock.
	oldNvidiaSmi := sysinfo.SetRunNvidiaSmiForTest(func() ([]byte, error) {
		return []byte("0, NVIDIA GB10, 0, 0, 12.1\n"), nil
	})
	defer oldNvidiaSmi()

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "unified memory")
	assert.Contains(t, out, "sm_121")
	assert.Contains(t, out, "hint")
	assert.Contains(t, out, "NIM_PASSTHROUGH_ARGS")
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

func TestStatusShowsAdhocContainers(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	injectAdhocRunner(t, &stubAdhoc{
		listResult: []provider.AdhocInfo{
			{Slug: "qwen-7b", Status: "running", Port: "8001", ID: "abc123def456789"},
		},
	})

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "ad-hoc containers:")
	assert.Contains(t, out, "qwen-7b")
	assert.Contains(t, out, "running")
}

func TestStatusShowsUnmanagedWarning(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	injectAdhocRunner(t, &stubAdhoc{
		unmanagedResult: []provider.UnmanagedContainer{
			{ID: "deadbeef1234567890", Image: "vllm/vllm-openai:latest", Names: []string{"rogue-vllm"}},
		},
	})

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "unmanaged inference containers")
	assert.Contains(t, out, "rogue-vllm")
}
