package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

func TestSystemdUnitContainsKeyFields(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnit(cfg, "vllm")

	assert.Contains(t, out, "[Unit]")
	assert.Contains(t, out, "[Service]")
	assert.Contains(t, out, "[Install]")
	assert.Contains(t, out, "vllm serve")
	assert.Contains(t, out, cfg.Paths.ActiveSymlink)
	assert.Contains(t, out, cfg.Server.Host)
	assert.Contains(t, out, "8000") // default port
	assert.Contains(t, out, cfg.Service.SystemdUnit)
}

func TestSystemdUnitSecretsEnvOptional(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnit(cfg, "vllm")
	// The secrets env file must be prefixed with '-' to make it optional.
	assert.Contains(t, out, "EnvironmentFile=-"+cfg.Paths.SecretsEnv)
}

func TestSystemdUnitPathDefault(t *testing.T) {
	cfg := config.Defaults()
	path := SystemdUnitPath(cfg)
	assert.Equal(t, "/etc/systemd/system/marlin.service", path)
}

func TestSystemdUnitPathCustomUnit(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.SystemdUnit = "my-vllm"
	assert.Equal(t, "/etc/systemd/system/my-vllm.service", SystemdUnitPath(cfg))
}

func TestSystemdUnitFullBinPath(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnit(cfg, "/home/dxa/venv/bin/vllm")
	assert.Contains(t, out, "/home/dxa/venv/bin/vllm serve")
	assert.NotContains(t, out, "exec vllm serve")
}

func TestResolveVLLMBinConfigured(t *testing.T) {
	bin, ok := ResolveVLLMBin("/opt/venv/bin/vllm")
	assert.Equal(t, "/opt/venv/bin/vllm", bin)
	assert.True(t, ok)
}

func TestResolveVLLMBinFallback(t *testing.T) {
	// When nothing is configured and vllm isn't in PATH (CI environment),
	// ResolveVLLMBin returns "vllm" with ok=false.
	bin, _ := ResolveVLLMBin("")
	// We can't assert ok since vllm may or may not be on CI PATH,
	// but the returned string must always be non-empty.
	assert.NotEmpty(t, bin)
}

func TestResolveVLLMBinSudoUserVenv(t *testing.T) {
	dir := t.TempDir()
	vllmBin := filepath.Join(dir, ".venv", "bin", "vllm")
	require.NoError(t, os.MkdirAll(filepath.Dir(vllmBin), 0o755))
	require.NoError(t, os.WriteFile(vllmBin, []byte("#!/bin/sh"), 0o755))

	orig := lookupUserHomeFunc
	t.Cleanup(func() { lookupUserHomeFunc = orig })
	lookupUserHomeFunc = func(string) (string, error) { return dir, nil }

	t.Setenv("SUDO_USER", "testuser")
	bin, ok := ResolveVLLMBin("")
	assert.Equal(t, vllmBin, bin)
	assert.True(t, ok)
}

func TestResolveVLLMBinSudoUserNotSet(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	// With no SUDO_USER and vllm not in PATH, should fall back gracefully.
	bin, _ := ResolveVLLMBin("")
	assert.NotEmpty(t, bin)
}

func TestSystemdUnitContainerizedKeyFields(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnitContainerized(cfg, "docker")
	assert.Contains(t, out, "[Unit]")
	assert.Contains(t, out, "docker run")
	assert.Contains(t, out, "--gpus all")
	assert.Contains(t, out, "--ipc host")
	assert.Contains(t, out, "--network host")
	assert.Contains(t, out, "--label marlin.managed=true")
	assert.Contains(t, out, "${VLLM_IMAGE}")
	assert.Contains(t, out, "--entrypoint vllm")
	assert.Contains(t, out, "8000")
	assert.NotContains(t, out, "-p 8000:8000") // network host replaces port mapping
	assert.Contains(t, out, cfg.Service.SystemdUnit)
}

// TestSystemdUnitContainerizedEntrypointVLLM verifies that the rendered unit sets
// --entrypoint vllm and passes "serve" exactly once, preventing double-invocation
// when the image's own ENTRYPOINT already includes "vllm serve".
func TestSystemdUnitContainerizedEntrypointVLLM(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnitContainerized(cfg, "docker")

	assert.Contains(t, out, "--entrypoint vllm")
	assert.Equal(t, 1, strings.Count(out, " serve "), "expected exactly one 'serve' token in ExecStart")
}

// TestSystemdUnitContainerizedNoDoubleServe is a regression guard: after ${VLLM_IMAGE}
// the immediate next token must be "serve", and no "vllm" token may follow the image.
func TestSystemdUnitContainerizedNoDoubleServe(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnitContainerized(cfg, "docker")

	const imgMarker = `"${VLLM_IMAGE}"`
	imgIdx := strings.Index(out, imgMarker)
	require.NotEqual(t, -1, imgIdx, "expected ${VLLM_IMAGE} in unit")

	afterImage := strings.TrimSpace(out[imgIdx+len(imgMarker):])
	assert.True(t, strings.HasPrefix(afterImage, "serve "),
		"expected 'serve' immediately after ${VLLM_IMAGE}, got: %q", afterImage)
	assert.NotContains(t, afterImage, "vllm",
		"no 'vllm' token should follow the image reference")
}

func TestSystemdUnitContainerizedFullBinPath(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnitContainerized(cfg, "/usr/bin/podman")
	assert.Contains(t, out, "/usr/bin/podman run")
	assert.NotContains(t, out, "exec docker run")
}

func TestResolveContainerBinConfiguredMissing(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "no-such-runtime-xyz"
	bin, ok := ResolveContainerBin(cfg)
	assert.Equal(t, "no-such-runtime-xyz", bin)
	assert.False(t, ok)
}

func TestResolveContainerBinFallback(t *testing.T) {
	cfg := config.Defaults()
	bin, _ := ResolveContainerBin(cfg)
	assert.NotEmpty(t, bin)
}

func TestEnvSingleLineExtraArgs(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{
			GPUMemoryUtilization: 0.9,
			ServedModelName:      []string{"gn100"},
			MaxModelLen:          131072,
		},
	}
	out := Env(m, "")
	// All args must appear on a single VLLM_EXTRA_ARGS line (no backslash continuation).
	for _, line := range splitLines(out) {
		if len(line) > 15 && line[:15] == "VLLM_EXTRA_ARGS" {
			assert.NotContains(t, line, "\\", "VLLM_EXTRA_ARGS must be single-line for systemd EnvironmentFile")
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
