package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	assert.True(t, cfg.Behavior.SwitchPrompt)
	assert.False(t, cfg.Behavior.AddAutoDetect)
	assert.Equal(t, 100, cfg.Behavior.LogTailLines)

	assert.Contains(t, cfg.Paths.ModelsDir, "marlin/models") // path varies by $HOME or falls back to /etc
	assert.Equal(t, "/etc/marlin/model.env", cfg.Paths.ActiveSymlink)
	assert.Contains(t, cfg.Paths.SecretsEnv, "secrets.env") // path varies by $HOME

	assert.Equal(t, "marlin", cfg.Service.SystemdUnit)
	assert.Equal(t, "marlin", cfg.Service.DockerContainer)

	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 8000, cfg.Server.Port)
	assert.Equal(t, "local", cfg.Server.Alias)

	assert.True(t, cfg.Registries.HuggingFace.Enabled)
	assert.True(t, cfg.Registries.NGC.Enabled)
	assert.False(t, cfg.Registries.ModelScope.Enabled)
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	require.NoError(t, err, "missing config file should return defaults, not error")
	assert.Equal(t, Defaults(), cfg)
}

func TestLoadOverridesDefaults(t *testing.T) {
	content := `
[behavior]
switch_prompt = false
log_tail_lines = 50

[server]
host = "gn100-01.lan"
port = 9000
alias = "blackwell"

[registries.modelscope]
enabled = true
`
	path := writeTempFile(t, "config.toml", content)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.Behavior.SwitchPrompt)
	assert.Equal(t, 50, cfg.Behavior.LogTailLines)
	assert.False(t, cfg.Behavior.AddAutoDetect, "unset fields should keep defaults")

	assert.Equal(t, "gn100-01.lan", cfg.Server.Host)
	assert.Equal(t, 9000, cfg.Server.Port)
	assert.Equal(t, "blackwell", cfg.Server.Alias)

	assert.True(t, cfg.Registries.ModelScope.Enabled)
}

func TestLoadInvalidTOML(t *testing.T) {
	path := writeTempFile(t, "bad.toml", "this is not [ valid toml %%")
	_, err := Load(path)
	assert.Error(t, err)
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
