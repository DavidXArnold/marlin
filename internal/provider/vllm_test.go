package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testVLLMProvider builds a VLLMProvider wired to a temp directory and an
// injectable systemd manager so tests never touch real systemd.
func testVLLMProvider(t *testing.T, execRunner service.ExecRunner) (*VLLMProvider, string) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0755))

	cfg := config.Defaults()
	cfg.Paths.ModelsDir = modelsDir
	cfg.Paths.ActiveSymlink = filepath.Join(dir, "model.env")
	cfg.Service.SystemdUnit = "vllm-test"

	p := NewVLLMProvider(cfg, []string{modelsDir}, "")
	p.svc = service.NewSystemdManagerWithRunner("vllm-test", execRunner)
	p.w = io.Discard // suppress privilege prompts in tests
	return p, dir
}

func writeTestModel(t *testing.T, dir, slug string) {
	t.Helper()
	m := &config.ModelConfig{
		Model: config.ModelMeta{
			Type: config.ProviderVLLM,
			ID:   "Qwen/Qwen2.5-72B-Instruct-AWQ",
		},
		Serve: config.ServeConfig{
			Quantization:         "awq_marlin",
			GPUMemoryUtilization: 0.90,
		},
	}
	require.NoError(t, config.SaveModel(filepath.Join(dir, slug+".toml"), m))
}

func successRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, nil
}

func failRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("failed"), fmt.Errorf("exit status 1")
}

// --- Switch ---

func TestSwitchSuccess(t *testing.T) {
	p, dir := testVLLMProvider(t, successRunner)
	writeTestModel(t, p.cfg.Paths.ModelsDir, "qwen25-72b")

	require.NoError(t, p.Switch(context.Background(), "qwen25-72b"))

	// Symlink must exist and point at the right env file
	target, err := os.Readlink(filepath.Join(dir, "model.env"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(p.cfg.Paths.ModelsDir, "qwen25-72b.env"), target)

	// Env file must contain the model ID
	content, err := os.ReadFile(filepath.Join(p.cfg.Paths.ModelsDir, "qwen25-72b.env"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Qwen/Qwen2.5-72B-Instruct-AWQ")
}

func TestSwitchModelNotFound(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	err := p.Switch(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading model")
}

func TestSwitchSymlinkFails(t *testing.T) {
	p, dir := testVLLMProvider(t, successRunner)
	writeTestModel(t, p.cfg.Paths.ModelsDir, "qwen25-72b")
	// Make the active symlink path a directory — atomicSymlink rename will fail.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "model.env"), 0755))

	err := p.Switch(context.Background(), "qwen25-72b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "updating active symlink")
}

func TestSwitchRestartFails(t *testing.T) {
	p, _ := testVLLMProvider(t, failRunner)
	writeTestModel(t, p.cfg.Paths.ModelsDir, "qwen25-72b")
	err := p.Switch(context.Background(), "qwen25-72b")
	assert.Error(t, err)
}

func TestSwitchUpdatesSymlinkAtomically(t *testing.T) {
	p, dir := testVLLMProvider(t, successRunner)
	writeTestModel(t, p.cfg.Paths.ModelsDir, "model-a")
	writeTestModel(t, p.cfg.Paths.ModelsDir, "model-b")

	require.NoError(t, p.Switch(context.Background(), "model-a"))
	require.NoError(t, p.Switch(context.Background(), "model-b"))

	target, err := os.Readlink(filepath.Join(dir, "model.env"))
	require.NoError(t, err)
	assert.Contains(t, target, "model-b.env")
}

// --- Stop ---

func TestStop(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	require.NoError(t, p.Stop(context.Background()))
}

func TestStopFails(t *testing.T) {
	p, _ := testVLLMProvider(t, failRunner)
	assert.Error(t, p.Stop(context.Background()))
}

// --- Status ---

func TestStatusActive(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	writeTestModel(t, p.cfg.Paths.ModelsDir, "qwen25-72b")
	require.NoError(t, p.Switch(context.Background(), "qwen25-72b"))

	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, s.Running)
	assert.Equal(t, "qwen25-72b", s.ModelID)
}

func TestStatusInactive(t *testing.T) {
	// inactiveRunner simulates systemctl exit code 3
	inactiveRunner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "is-active" {
				return []byte("inactive"), &exitCode3{}
			}
		}
		return nil, nil
	}
	p, _ := testVLLMProvider(t, inactiveRunner)

	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, s.Running)
	assert.Empty(t, s.ModelID)
}

func TestStatusNoSymlink(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	// Service is "active" but symlink doesn't exist yet — ModelID should be empty
	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, s.Running)
	assert.Empty(t, s.ModelID)
}

// --- Logs ---

func TestLogs(t *testing.T) {
	var buf bytes.Buffer
	called := false
	old := runCommand
	runCommand = func(_ context.Context, w io.Writer, name string, args ...string) error {
		called = true
		_, _ = fmt.Fprint(w, "log line 1\nlog line 2\n")
		return nil
	}
	defer func() { runCommand = old }()

	p, _ := testVLLMProvider(t, successRunner)
	require.NoError(t, p.Logs(context.Background(), &buf, false, 50))
	assert.True(t, called)
	assert.Contains(t, buf.String(), "log line 1")
}

func TestLogsFollow(t *testing.T) {
	var capturedArgs []string
	old := runCommand
	runCommand = func(_ context.Context, _ io.Writer, _ string, args ...string) error {
		capturedArgs = args
		return nil
	}
	defer func() { runCommand = old }()

	p, _ := testVLLMProvider(t, successRunner)
	require.NoError(t, p.Logs(context.Background(), io.Discard, true, 100))
	assert.Contains(t, capturedArgs, "-f")
}

func TestLogsError(t *testing.T) {
	old := runCommand
	runCommand = func(_ context.Context, _ io.Writer, _ string, _ ...string) error {
		return fmt.Errorf("journalctl not found")
	}
	defer func() { runCommand = old }()

	p, _ := testVLLMProvider(t, successRunner)
	assert.Error(t, p.Logs(context.Background(), io.Discard, false, 50))
}

func TestStatusError(t *testing.T) {
	p, _ := testVLLMProvider(t, failRunner)
	_, err := p.Status(context.Background())
	assert.Error(t, err)
}

func TestSwitchEnvFileWriteFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	p, _ := testVLLMProvider(t, successRunner)
	writeTestModel(t, p.cfg.Paths.ModelsDir, "qwen25-72b")
	require.NoError(t, os.Chmod(p.cfg.Paths.ModelsDir, 0555))
	defer func() { _ = os.Chmod(p.cfg.Paths.ModelsDir, 0755) }()

	err := p.Switch(context.Background(), "qwen25-72b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "writing env file")
}

// --- atomicSymlink helpers ---

func TestAtomicSymlinkCreatesParent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0644))

	linkPath := filepath.Join(dir, "sub", "link.env")
	require.NoError(t, atomicSymlink(target, linkPath))

	got, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, target, got)
}

func TestWriteEnvFileMkdirFails(t *testing.T) {
	dir := t.TempDir()
	// Place a regular file where a directory is expected so MkdirAll fails.
	blocker := filepath.Join(dir, "notadir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))

	err := writeEnvFile(filepath.Join(blocker, "sub", "model.env"), "content")
	assert.Error(t, err)
}

func TestAtomicSymlinkMkdirFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0644))

	// Place a regular file where atomicSymlink would need to create a directory.
	blocker := filepath.Join(dir, "notadir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))

	err := atomicSymlink(target, filepath.Join(blocker, "active.env"))
	assert.Error(t, err)
}

func TestAtomicSymlinkRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0644))

	// Make linkPath a directory — renaming a symlink onto a directory fails.
	linkPath := filepath.Join(dir, "active.env")
	require.NoError(t, os.MkdirAll(linkPath, 0755))

	err := atomicSymlink(target, linkPath)
	assert.Error(t, err)
}

func TestAtomicSymlinkReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	targetA := filepath.Join(dir, "a.env")
	targetB := filepath.Join(dir, "b.env")
	require.NoError(t, os.WriteFile(targetA, []byte("a"), 0644))
	require.NoError(t, os.WriteFile(targetB, []byte("b"), 0644))

	link := filepath.Join(dir, "active.env")
	require.NoError(t, atomicSymlink(targetA, link))
	require.NoError(t, atomicSymlink(targetB, link))

	got, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, targetB, got)
}

func TestAtomicSymlinkSymlinkFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0644))

	linkDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.MkdirAll(linkDir, 0555))
	defer func() { _ = os.Chmod(linkDir, 0755) }()

	err := atomicSymlink(target, filepath.Join(linkDir, "active.env"))
	assert.Error(t, err)
}

// exitCode3 satisfies the exitCoder interface used by service.SystemdManager.
type exitCode3 struct{}

func (e *exitCode3) Error() string { return "exit status 3" }
func (e *exitCode3) ExitCode() int { return 3 }
