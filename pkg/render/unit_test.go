package render

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
