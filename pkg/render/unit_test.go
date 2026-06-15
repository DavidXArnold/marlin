package render

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DavidXArnold/marlin/internal/config"
)

func TestSystemdUnitContainsKeyFields(t *testing.T) {
	cfg := config.Defaults()
	out := SystemdUnit(cfg)

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
	out := SystemdUnit(cfg)
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

func TestEnvSingleLineExtraArgs(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{
			GPUMemoryUtilization: 0.9,
			ServedModelName:      []string{"gn100"},
			MaxModelLen:          131072,
		},
	}
	out := Env(m)
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
