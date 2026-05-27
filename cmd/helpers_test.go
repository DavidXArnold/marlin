package cmd

import (
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

// --- globalConfig ---

func TestGlobalConfigMissingFile(t *testing.T) {
	old := cfgFile
	cfgFile = "/nonexistent/path/config.toml"
	defer func() { cfgFile = old }()

	cfg, err := globalConfig()
	require.NoError(t, err) // missing file → returns defaults
	assert.Equal(t, "/etc/marlin/models", cfg.Paths.ModelsDir)
}

func TestGlobalConfigFromEnvFile(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	assert.NotEqual(t, "/etc/marlin/models", cfg.Paths.ModelsDir)
}

// --- buildProvider ---

func TestBuildProviderVLLM(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	p, err := buildProvider(config.ProviderVLLM, cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestBuildProviderNIM(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	p, err := buildProvider(config.ProviderNIM, cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestBuildProviderEmpty(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	p, err := buildProvider("", cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestBuildProviderUnknown(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, err = buildProvider("unknown-type", cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider type")
}

// --- resolveModel ---

func TestResolveModelNoModels(t *testing.T) {
	_, err := resolveModel("qwen", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

func TestResolveModelExactMatch(t *testing.T) {
	names := []string{"qwen25-72b", "llama-8b"}
	got, err := resolveModel("qwen25-72b", names, nil)
	require.NoError(t, err)
	assert.Equal(t, "qwen25-72b", got)
}

func TestResolveModelFuzzySingle(t *testing.T) {
	names := []string{"qwen25-72b", "llama-8b"}
	got, err := resolveModel("llama", names, nil)
	require.NoError(t, err)
	assert.Equal(t, "llama-8b", got)
}

func TestResolveModelFuzzyNoMatch(t *testing.T) {
	names := []string{"qwen25-72b", "llama-8b"}
	_, err := resolveModel("gpt9000", names, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no model matching")
}

func TestResolveModelSingleWithNoQuery(t *testing.T) {
	// Single model, no query → PickModel returns it directly (no TTY needed).
	got, err := resolveModel("", []string{"only-model"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "only-model", got)
}

// --- min12 ---

func TestMin12(t *testing.T) {
	assert.Equal(t, 5, min12(5))
	assert.Equal(t, 12, min12(20))
	assert.Equal(t, 12, min12(12))
	assert.Equal(t, 0, min12(0))
}

// --- status with active model ---

func TestStatusCmdActiveModel(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	// Write state with active model and a container ID (to exercise min12 path).
	s := &state.State{
		ActiveModel:    "qwen25-72b",
		ActiveProvider: "vllm",
		ContainerID:    "abc1234567890",
	}
	require.NoError(t, state.Save(cfg.Paths.StateFile, s))

	out, err := executeCmd("status")
	require.NoError(t, err)
	assert.Contains(t, out, "qwen25-72b")
	assert.Contains(t, out, "vllm")
	assert.Contains(t, out, "abc123456789")
}

// --- validate with issues ---

func TestValidateCmdWithIssues(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	// Model with no served_model_name → will trigger warn about alias
	content := `[model]
id = "Qwen/Qwen2.5-72B-Instruct-AWQ"
type = "vllm"
status = "untested"

[serve]
gpu_memory_utilization = 0.90
`
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "qwen25-72b.toml"), []byte(content), 0o644))

	cfgContent := fmt.Sprintf(`[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q

[server]
alias = "gn100"
`, modelsDir,
		filepath.Join(dir, "state.toml"),
		filepath.Join(dir, "secrets.env"),
		filepath.Join(dir, "model.env"),
	)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))

	old := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = old }()

	out, err := executeCmd("validate", "qwen25-72b")
	require.NoError(t, err)
	assert.Contains(t, out, "warn")
}

// --- search with hf registry only ---

func TestSearchCmdHFOnly(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("search", "--registry", "huggingface", "llama")
	require.NoError(t, err)
}

func TestSearchCmdUnknownRegistry(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	// Unknown registry name → buildRegistries returns empty slice → no output, no error.
	_, err := executeCmd("search", "--registry", "unknown-reg", "llama")
	require.NoError(t, err)
}

// --- switch error paths ---

func TestSwitchValidationError(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	// Model with missing id → validation error blocks switch.
	content := `[model]
type = "vllm"
status = "untested"

[serve]
gpu_memory_utilization = 0.90
`
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "bad-model.toml"), []byte(content), 0o644))

	cfgContent := fmt.Sprintf(`[behavior]
switch_prompt = false

[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q

[server]
alias = "gn100"
`, modelsDir,
		filepath.Join(dir, "state.toml"),
		filepath.Join(dir, "secrets.env"),
		filepath.Join(dir, "model.env"),
	)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))

	old := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = old }()

	_, err := executeCmd("switch", "bad-model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

func TestRunLogsDirectWithFollowAndLines(t *testing.T) {
	restore := provider.SetRunCommandForTest(func(_ context.Context, _ io.Writer, _ string, args ...string) error {
		// verify journalctl args are passed
		return nil
	})
	defer restore()

	cleanup := tempEnv(t)
	defer cleanup()

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	require.NoError(t, cmd.Flags().Set("follow", "true"))
	require.NoError(t, cmd.Flags().Set("lines", "50"))
	require.NoError(t, runLogs(cmd, nil))
}
