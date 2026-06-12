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

// mockProv is a test-only Provider that records calls and returns canned errors.
type mockProv struct {
	switchErr     error
	stopErr       error
	stopCalled    bool
	stopFn        func()
	statusRunning bool // if true, Status returns Running: true
}

func (m *mockProv) Switch(_ context.Context, _ string) error { return m.switchErr }
func (m *mockProv) Stop(_ context.Context) error {
	m.stopCalled = true
	if m.stopFn != nil {
		m.stopFn()
	}
	return m.stopErr
}
func (m *mockProv) Status(_ context.Context) (*provider.Status, error) {
	return &provider.Status{Running: m.statusRunning}, nil
}
func (m *mockProv) Logs(_ context.Context, _ io.Writer, _ bool, _ int) error { return nil }

// injectProvider replaces buildProvider with one that always returns p and restores it on cleanup.
func injectProvider(t *testing.T, p provider.Provider) {
	t.Helper()
	old := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return p, nil
	}
	t.Cleanup(func() { buildProvider = old })
}

// noopRequireRoot is a no-op kept for call-site compatibility; privilege
// escalation is now handled inline by the privilege package.
func noopRequireRoot(_ *testing.T) {}

// switchEnv creates a temp config with switch_prompt=false and returns the
// models dir path and cleanup func.
func switchEnv(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	cfgContent := fmt.Sprintf(`[behavior]
switch_prompt = false
allow_type_switch = true

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
	return modelsDir, func() { cfgFile = old }
}

func writeVLLMModel(t *testing.T, modelsDir, slug string) {
	t.Helper()
	content := fmt.Sprintf(`[model]
id = "%s-model-id"
type = "vllm"
status = "untested"

[serve]
gpu_memory_utilization = 0.90
served_model_name = ["gn100"]
`, slug)
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, slug+".toml"), []byte(content), 0o644))
}

// TestSwitchAllowTypeSwitchFalse tests the early error before RequireRoot is called.
func TestSwitchAllowTypeSwitchFalse(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	content := `[model]
id = "llama-model-id"
type = "vllm"
status = "untested"

[serve]
gpu_memory_utilization = 0.90
served_model_name = ["gn100"]
`
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "llama-8b.toml"), []byte(content), 0o644))

	cfgContent := fmt.Sprintf(`[behavior]
switch_prompt = false
allow_type_switch = false

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

	// Set state with active nim model so type switch would be needed.
	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "old-nim-model",
		ActiveProvider: config.ProviderNIM,
	}))

	old := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = old }()

	_, err = executeCmd("switch", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestRunSwitchSuccess covers the happy path: requireRoot no-op, mock provider succeeds.
func TestRunSwitchSuccess(t *testing.T) {
	noopRequireRoot(t)
	mock := &mockProv{}
	injectProvider(t, mock)

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	out, err := executeCmd("switch", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
	assert.Contains(t, out, "llama-8b")
}

// TestRunSwitchValidationWarning covers the warning print branch (non-error issues).
func TestRunSwitchValidationWarning(t *testing.T) {
	noopRequireRoot(t)
	injectProvider(t, &mockProv{})

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()

	// Model with no served_model_name → validate.Model emits a warning (not error).
	content := `[model]
id = "test/model-id"
type = "vllm"
status = "untested"

[serve]
gpu_memory_utilization = 0.90
`
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "warn-model.toml"), []byte(content), 0o644))

	out, err := executeCmd("switch", "warn-model")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
}

// TestRunSwitchProviderBuildError covers the buildProvider failure branch.
func TestRunSwitchProviderBuildError(t *testing.T) {
	noopRequireRoot(t)

	old := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return nil, fmt.Errorf("docker not available")
	}
	t.Cleanup(func() { buildProvider = old })

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	_, err := executeCmd("switch", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker not available")
}

// TestRunSwitchSwitchError covers the p.Switch() failure branch.
func TestRunSwitchSwitchError(t *testing.T) {
	noopRequireRoot(t)
	injectProvider(t, &mockProv{switchErr: fmt.Errorf("systemd failed")})

	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	_, err := executeCmd("switch", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemd failed")
}

// TestRunSwitchStopsOldProviderOnTypeChange covers the old-provider Stop block.
func TestRunSwitchStopsOldProviderOnTypeChange(t *testing.T) {
	noopRequireRoot(t)

	oldMock := &mockProv{}
	newMock := &mockProv{}
	callCount := 0
	old := buildProvider
	buildProvider = func(pt config.ProviderType, cfg *config.Config) (provider.Provider, error) {
		callCount++
		if callCount == 1 {
			return oldMock, nil // old provider (stop)
		}
		return newMock, nil // new provider (switch)
	}
	t.Cleanup(func() { buildProvider = old })

	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))
	writeVLLMModel(t, modelsDir, "llama-8b")

	cfgContent := fmt.Sprintf(`[behavior]
switch_prompt = false
allow_type_switch = true

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

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "old-nim-model",
		ActiveProvider: config.ProviderNIM,
	}))

	oldCfg := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = oldCfg })

	out, err := executeCmd("switch", "llama-8b")
	require.NoError(t, err)
	assert.True(t, oldMock.stopCalled, "Stop should be called on old provider")
	assert.Contains(t, out, "switched to")
}

// TestGlobalConfigNoCandidates covers the candidate-search code path when cfgFile is empty.
func TestGlobalConfigNoCandidates(t *testing.T) {
	old := cfgFile
	cfgFile = ""
	defer func() { cfgFile = old }()

	// Neither /etc/marlin/config.toml nor ~/.config/marlin/config.toml exist on
	// this machine in CI, so globalConfig falls back to defaults.
	cfg, err := globalConfig()
	require.NoError(t, err)
	// Either defaults (when no candidate exists) or a loaded config — both are valid.
	assert.NotNil(t, cfg)
}
