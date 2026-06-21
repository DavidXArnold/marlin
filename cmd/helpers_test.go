package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

// --- globalConfig ---

func TestGlobalConfigMissingFile(t *testing.T) {
	old := cfgFile
	cfgFile = "/nonexistent/path/config.toml"
	defer func() { cfgFile = old }()

	cfg, err := globalConfig()
	require.NoError(t, err) // missing file → returns defaults
	assert.Contains(t, cfg.Paths.ModelsDir, "marlin/models") // varies by $HOME
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
	_, err := resolveModel("qwen", nil, nil, "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

func TestResolveModelExactMatch(t *testing.T) {
	names := []string{"qwen25-72b", "llama-8b"}
	got, err := resolveModel("qwen25-72b", names, nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "qwen25-72b", got)
}

func TestResolveModelFuzzySingle(t *testing.T) {
	names := []string{"qwen25-72b", "llama-8b"}
	got, err := resolveModel("llama", names, nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "llama-8b", got)
}

func TestResolveModelFuzzyNoMatch(t *testing.T) {
	names := []string{"qwen25-72b", "llama-8b"}
	_, err := resolveModel("gpt9000", names, nil, "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no model matching")
}

func TestResolveModelSingleWithNoQuery(t *testing.T) {
	// Single model, no query → PickModel returns it directly (no TTY needed).
	got, err := resolveModel("", []string{"only-model"}, nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "only-model", got)
}

// --- installDir ---

func TestInstallDirDefault(t *testing.T) {
	cfg, _ := globalConfig()
	dir := installDir(cfg, false)
	assert.Equal(t, cfg.Paths.ModelsDir, dir)
}

func TestInstallDirGlobalFlag(t *testing.T) {
	cfg, _ := globalConfig()
	dir := installDir(cfg, true)
	assert.Equal(t, cfg.Paths.GlobalModelsDir, dir)
}

func TestInstallDirGlobalConfig(t *testing.T) {
	cfg, _ := globalConfig()
	cfg.Behavior.GlobalInstall = true
	dir := installDir(cfg, false)
	assert.Equal(t, cfg.Paths.GlobalModelsDir, dir)
}

// --- checkSystemResources ---

func TestCheckSystemResourcesDisabled(t *testing.T) {
	cfg, _ := globalConfig()
	cfg.Behavior.WarnOnSystemResources = false
	var buf strings.Builder
	checkSystemResources(cfg, &buf)
	assert.Empty(t, buf.String())
}

func TestCheckSystemResourcesEnabled(t *testing.T) {
	cfg, _ := globalConfig()
	cfg.Behavior.WarnOnSystemResources = true
	cfg.Behavior.SystemLoadThreshold = 0.8
	var buf strings.Builder
	// On this platform, LoadAvg1() may return 0 (no /proc/loadavg) — the function
	// returns early without writing. Either way, it must not panic.
	checkSystemResources(cfg, &buf)
}

// --- shortID ---

func TestShortID(t *testing.T) {
	assert.Equal(t, "abc12", shortID("abc12"))
	assert.Equal(t, "abcdef012345", shortID("abcdef0123456789"))
	assert.Equal(t, "abcdef012345", shortID("abcdef012345"))
	assert.Equal(t, "", shortID(""))
}

// --- status with active model ---

func TestStatusCmdActiveModel(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	// Write state with active model and a container ID (to exercise shortID path).
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
		return nil
	})
	defer restore()
	injectManagedLogsTarget(t)

	cleanup := tempEnv(t)
	defer cleanup()

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	require.NoError(t, cmd.Flags().Set("follow", "true"))
	require.NoError(t, cmd.Flags().Set("lines", "50"))
	require.NoError(t, runLogs(cmd, nil))
}

// --- maybeOfferUMAHint ---

func TestMaybeOfferUMAHintNotNIM(t *testing.T) {
	mc := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderVLLM}}
	old := umaHintConfirmFunc
	umaHintConfirmFunc = func(string) (bool, error) {
		t.Fatal("confirm should not be called for non-NIM profile")
		return false, nil
	}
	defer func() { umaHintConfirmFunc = old }()
	var buf bytes.Buffer
	maybeOfferUMAHint(mc, &buf)
	assert.Empty(t, mc.Serve.ExtraEnv)
}

func TestMaybeOfferUMAHintAlreadySet(t *testing.T) {
	mc := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderNIM},
		Serve: config.ServeConfig{ExtraEnv: []string{"NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.5"}},
	}
	old := umaHintConfirmFunc
	umaHintConfirmFunc = func(string) (bool, error) {
		t.Fatal("confirm should not be called when already set")
		return false, nil
	}
	defer func() { umaHintConfirmFunc = old }()
	var buf bytes.Buffer
	maybeOfferUMAHint(mc, &buf)
	assert.Len(t, mc.Serve.ExtraEnv, 1) // unchanged
}

func TestMaybeOfferUMAHintNoUMA(t *testing.T) {
	restore := sysinfo.SetRunNvidiaSmiForTest(func() ([]byte, error) {
		return []byte("0, NVIDIA A100-SXM4-80GB, 81920, 75000, 8.0\n"), nil
	})
	defer restore()

	mc := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderNIM}}
	old := umaHintConfirmFunc
	umaHintConfirmFunc = func(string) (bool, error) {
		t.Fatal("confirm should not be called when no UMA GPU")
		return false, nil
	}
	defer func() { umaHintConfirmFunc = old }()
	var buf bytes.Buffer
	maybeOfferUMAHint(mc, &buf)
	assert.Empty(t, mc.Serve.ExtraEnv)
}

func TestMaybeOfferUMAHintUMAConfirmYes(t *testing.T) {
	restore := sysinfo.SetRunNvidiaSmiForTest(func() ([]byte, error) {
		return []byte("0, NVIDIA GB10, 0, 0, 12.1\n"), nil
	})
	defer restore()

	mc := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderNIM}}
	old := umaHintConfirmFunc
	umaHintConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { umaHintConfirmFunc = old }()

	var buf bytes.Buffer
	maybeOfferUMAHint(mc, &buf)
	require.Len(t, mc.Serve.ExtraEnv, 1)
	assert.Contains(t, mc.Serve.ExtraEnv[0], "NIM_PASSTHROUGH_ARGS")
	assert.Contains(t, buf.String(), "UMA")
}

func TestMaybeOfferUMAHintUMAConfirmNo(t *testing.T) {
	restore := sysinfo.SetRunNvidiaSmiForTest(func() ([]byte, error) {
		return []byte("0, NVIDIA GB10, 0, 0, 12.1\n"), nil
	})
	defer restore()

	mc := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderNIM}}
	old := umaHintConfirmFunc
	umaHintConfirmFunc = func(string) (bool, error) { return false, nil }
	defer func() { umaHintConfirmFunc = old }()

	var buf bytes.Buffer
	maybeOfferUMAHint(mc, &buf)
	assert.Empty(t, mc.Serve.ExtraEnv) // not added when user declines
}

// --- effectiveMaxRuntime ---

func TestEffectiveMaxRuntimeFromFlag(t *testing.T) {
	cfg := config.Defaults()
	cfg.Behavior.MaxRuntime = "5m"

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().String("max-runtime", "", "")
	require.NoError(t, cmd.Flags().Set("max-runtime", "30s"))

	// Flag takes precedence over config.
	d := effectiveMaxRuntime(cmd, cfg)
	assert.Equal(t, 30*time.Second, d)
}

func TestEffectiveMaxRuntimeFromConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Behavior.MaxRuntime = "15m"

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().String("max-runtime", "", "")
	// Flag not set → falls back to config.
	d := effectiveMaxRuntime(cmd, cfg)
	assert.Equal(t, 15*time.Minute, d)
}

func TestEffectiveMaxRuntimeZeroWhenUnset(t *testing.T) {
	cfg := config.Defaults() // MaxRuntime == ""
	cmd := cmdWithContext(io.Discard)
	cmd.Flags().String("max-runtime", "", "")
	assert.Equal(t, time.Duration(0), effectiveMaxRuntime(cmd, cfg))
}

// --- humanDuration ---

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "less than a minute"},
		{time.Minute, "1 minute"},
		{5 * time.Minute, "5 minutes"},
		{time.Hour, "1 hour"},
		{3 * time.Hour, "3 hours"},
		{24 * time.Hour, "1 day"},
		{48 * time.Hour, "2 days"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, humanDuration(tc.d), "duration %v", tc.d)
	}
}

// --- smokeConfig ---

func TestSmokeConfigDefaults(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)
	sc := smokeConfig(cfg)
	assert.False(t, sc.Enabled)
	assert.Equal(t, 30*time.Second, sc.Timeout)
	assert.Empty(t, sc.Skip)
}

func TestSmokeConfigCustomTimeout(t *testing.T) {
	cleanup := tempEnvWithBehavior(t, `smoke_test = true
smoke_test_timeout = "10s"
smoke_test_skip = ["streaming"]`)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)
	sc := smokeConfig(cfg)
	assert.True(t, sc.Enabled)
	assert.Equal(t, 10*time.Second, sc.Timeout)
	assert.Equal(t, []string{"streaming"}, sc.Skip)
}
