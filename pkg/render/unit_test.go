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

func TestSystemdUnitsLimitCrashLoops(t *testing.T) {
	cfg := config.Defaults()
	for name, unit := range map[string]string{
		"binary":        SystemdUnit(cfg, "/usr/bin/vllm"),
		"containerized": SystemdUnitContainerized(cfg, "/usr/bin/docker"),
		"llamacpp":      SystemdUnitLlamaCpp(cfg, "/usr/bin/llama-server"),
		"llamacpp-ctr":  SystemdUnitLlamaCppContainerized(cfg, "/usr/bin/docker", "/hf"),
	} {
		assert.Contains(t, unit, "StartLimitIntervalSec=300", name)
		assert.Contains(t, unit, "StartLimitBurst=5", name)
		// Start limits belong in [Unit]; in [Service] systemd ignores them.
		assert.Less(t, strings.Index(unit, "StartLimitBurst"), strings.Index(unit, "[Service]"), name)
	}
	c := SystemdUnitContainerized(cfg, "/usr/bin/docker")
	assert.Contains(t, c, "ExecStartPre=-/usr/bin/docker rm -f marlin-vllm")
	assert.Less(t, strings.Index(c, "ExecStartPre"), strings.Index(c, "ExecStart="))
}

func TestSystemdUnitLlamaCppContainerized(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Port = 8123
	u := SystemdUnitLlamaCppContainerized(cfg, "/usr/bin/docker", "/home/u/.cache/huggingface")

	assert.Contains(t, u, "EnvironmentFile=/etc/marlin/llamacpp.env")
	assert.Contains(t, u, "ExecStartPre=-/usr/bin/docker rm -f marlin-llamacpp")
	assert.Contains(t, u, "exec /usr/bin/docker run --rm --gpus all --ipc host --network host --name marlin-llamacpp")
	assert.Contains(t, u, "--label marlin.managed=true")
	assert.Contains(t, u, `-v "/home/u/.cache/huggingface:/root/.cache/huggingface"`)
	assert.Contains(t, u, `-e "HF_TOKEN=${HF_TOKEN:-}"`)
	assert.Contains(t, u, `${LLAMA_CONTAINER_ARGS:-} "${LLAMA_IMAGE}" ${LLAMA_MODEL_ARGS} --host 0.0.0.0 --port 8123 ${LLAMA_EXTRA_ARGS:-}`)
	// Health check must follow the configured port, not the image's 8080 default.
	assert.Contains(t, u, `--health-cmd "curl -fsS http://localhost:8123/health || exit 1"`)
	assert.Contains(t, u, "SyslogIdentifier=marlin-llamacpp")
}

func TestSystemdUnitLlamaCppContainerizedNerdctlNoHealthCmd(t *testing.T) {
	u := SystemdUnitLlamaCppContainerized(config.Defaults(), "/usr/local/bin/nerdctl", "/hf")
	assert.NotContains(t, u, "--health-cmd")
}

func TestSystemdUnitLlamaCppBinary(t *testing.T) {
	cfg := config.Defaults()
	u := SystemdUnitLlamaCpp(cfg, "/opt/llama/llama-server")
	assert.Contains(t, u, "exec /opt/llama/llama-server ${LLAMA_MODEL_ARGS} --host localhost --port 8000 ${LLAMA_EXTRA_ARGS:-}")
	assert.NotContains(t, u, "LLAMA_IMAGE")
}

func TestLlamaCppUnitPath(t *testing.T) {
	cfg := config.Defaults()
	assert.Equal(t, "/etc/systemd/system/marlin-llamacpp.service", LlamaCppUnitPath(cfg))
}

func TestResolveLlamaCppBin(t *testing.T) {
	got, ok := ResolveLlamaCppBin("/x/llama-server")
	assert.True(t, ok)
	assert.Equal(t, "/x/llama-server", got)

	t.Setenv("PATH", t.TempDir())
	got, ok = ResolveLlamaCppBin("")
	assert.False(t, ok)
	assert.Equal(t, "llama-server", got)
}

func TestResolveHFCache(t *testing.T) {
	cfg := config.Defaults()
	cfg.Paths.HFCache = "/data/hf"
	assert.Equal(t, "/data/hf", ResolveHFCache(cfg))

	cfg.Paths.HFCache = ""
	orig := lookupUserHomeFunc
	t.Cleanup(func() { lookupUserHomeFunc = orig })
	lookupUserHomeFunc = func(string) (string, error) { return "/home/alice", nil }
	t.Setenv("SUDO_USER", "alice")
	assert.Equal(t, "/home/alice/.cache/huggingface", ResolveHFCache(cfg))

	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Equal(t, filepath.Join(home, ".cache", "huggingface"), ResolveHFCache(cfg))
}

func TestLlamaCppEnvFileFallback(t *testing.T) {
	cfg := config.Defaults()
	cfg.Paths.LlamaCppEnvFile = ""
	assert.Contains(t, SystemdUnitLlamaCpp(cfg, "llama-server"), "EnvironmentFile=/etc/marlin/llamacpp.env")
}
