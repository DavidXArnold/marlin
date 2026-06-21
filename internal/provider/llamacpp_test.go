package provider

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
	"github.com/DavidXArnold/marlin/internal/service"
)

func testLlamaCppProvider(t *testing.T, runner service.ExecRunner) (*LlamaCppProvider, string) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0755))

	cfg := config.Defaults()
	cfg.Paths.ModelsDir = modelsDir
	cfg.Paths.LlamaCppEnvFile = filepath.Join(dir, "llamacpp.env")
	cfg.Service.LlamaCppUnit = "llamacpp-test"

	p := NewLlamaCppProvider(cfg, []string{modelsDir})
	p.svc = service.NewSystemdManagerWithRunner("llamacpp-test", runner)
	p.w = io.Discard
	return p, dir
}

func writeLlamaCppModel(t *testing.T, dir, slug, ggufPath string) {
	t.Helper()
	m := &config.ModelConfig{
		Model: config.ModelMeta{
			Type: config.ProviderLlamaCpp,
			ID:   slug,
		},
		Serve: config.ServeConfig{
			GGUFPath:    ggufPath,
			NGL:         99,
			ContextSize: 4096,
		},
	}
	require.NoError(t, config.SaveModel(filepath.Join(dir, slug+".toml"), m))
}

// --- Switch ---

func TestLlamaCppSwitchSuccess(t *testing.T) {
	p, dir := testLlamaCppProvider(t, successRunner)
	gguf := filepath.Join(dir, "llama-3.2-3b-q4.gguf")
	writeLlamaCppModel(t, p.cfg.Paths.ModelsDir, "llama-3b", gguf)

	require.NoError(t, p.Switch(context.Background(), "llama-3b"))

	// Symlink must point at the slug's env file.
	target, err := os.Readlink(p.cfg.Paths.LlamaCppEnvFile)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(p.cfg.Paths.ModelsDir, "llama-3b.llamacpp.env"), target)

	// Env file must contain the GGUF path and NGL.
	content, err := os.ReadFile(filepath.Join(p.cfg.Paths.ModelsDir, "llama-3b.llamacpp.env"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "LLAMA_MODEL="+gguf)
	assert.Contains(t, string(content), "LLAMA_NGL=99")
	assert.Contains(t, string(content), "LLAMA_CONTEXT=4096")
}

func TestLlamaCppSwitchNoGGUFPath(t *testing.T) {
	p, _ := testLlamaCppProvider(t, successRunner)
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderLlamaCpp, ID: "no-path"},
		Serve: config.ServeConfig{},
	}
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "no-path.toml"), m))

	err := p.Switch(context.Background(), "no-path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gguf_path")
}

func TestLlamaCppSwitchModelNotFound(t *testing.T) {
	p, _ := testLlamaCppProvider(t, successRunner)
	err := p.Switch(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading model")
}

func TestLlamaCppSwitchRestartFails(t *testing.T) {
	p, dir := testLlamaCppProvider(t, failRunner)
	gguf := filepath.Join(dir, "model.gguf")
	writeLlamaCppModel(t, p.cfg.Paths.ModelsDir, "llama-3b", gguf)
	err := p.Switch(context.Background(), "llama-3b")
	assert.Error(t, err)
}

func TestLlamaCppSwitchDefaultNGL(t *testing.T) {
	p, dir := testLlamaCppProvider(t, successRunner)
	gguf := filepath.Join(dir, "model.gguf")
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderLlamaCpp, ID: "llama-3b"},
		Serve: config.ServeConfig{GGUFPath: gguf},
	}
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "llama-3b.toml"), m))

	require.NoError(t, p.Switch(context.Background(), "llama-3b"))

	content, err := os.ReadFile(filepath.Join(p.cfg.Paths.ModelsDir, "llama-3b.llamacpp.env"))
	require.NoError(t, err)
	// Default NGL is 99 when not set.
	assert.Contains(t, string(content), "LLAMA_NGL=99")
}

// --- Stop ---

func TestLlamaCppStop(t *testing.T) {
	p, _ := testLlamaCppProvider(t, successRunner)
	require.NoError(t, p.Stop(context.Background()))
}

func TestLlamaCppStopFails(t *testing.T) {
	p, _ := testLlamaCppProvider(t, failRunner)
	assert.Error(t, p.Stop(context.Background()))
}

// --- Status ---

func TestLlamaCppStatusActive(t *testing.T) {
	p, dir := testLlamaCppProvider(t, successRunner)
	gguf := filepath.Join(dir, "model.gguf")
	writeLlamaCppModel(t, p.cfg.Paths.ModelsDir, "llama-3b", gguf)
	require.NoError(t, p.Switch(context.Background(), "llama-3b"))

	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, s.Running)
	assert.Equal(t, "llama-3b", s.ModelID)
}

func TestLlamaCppStatusInactive(t *testing.T) {
	inactiveRunner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "is-active" {
				return []byte("inactive"), &exitCode3{}
			}
		}
		return nil, nil
	}
	p, _ := testLlamaCppProvider(t, inactiveRunner)

	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, s.Running)
	assert.Empty(t, s.ModelID)
}

func TestLlamaCppStatusNoSymlink(t *testing.T) {
	p, _ := testLlamaCppProvider(t, successRunner)
	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, s.Running)
	assert.Empty(t, s.ModelID)
}

// --- Logs ---

func TestLlamaCppLogs(t *testing.T) {
	var buf bytes.Buffer
	called := false
	old := runCommand
	runCommand = func(_ context.Context, w io.Writer, name string, args ...string) error {
		called = true
		_, _ = fmt.Fprint(w, "llama log line\n")
		return nil
	}
	defer func() { runCommand = old }()

	p, _ := testLlamaCppProvider(t, successRunner)
	require.NoError(t, p.Logs(context.Background(), &buf, false, 50))
	assert.True(t, called)
	assert.Contains(t, buf.String(), "llama log line")
}

func TestLlamaCppLogsFollow(t *testing.T) {
	var gotArgs []string
	old := runCommand
	runCommand = func(_ context.Context, w io.Writer, name string, args ...string) error {
		gotArgs = args
		return nil
	}
	defer func() { runCommand = old }()

	p, _ := testLlamaCppProvider(t, successRunner)
	require.NoError(t, p.Logs(context.Background(), io.Discard, true, 50))
	assert.Contains(t, gotArgs, "-f")
}
