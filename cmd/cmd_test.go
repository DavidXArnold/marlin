package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/ui"
)

// tempEnv writes a minimal config file pointing at a temp directory.
// If slugs are provided, a .toml model file is created for each.
// Returns a cleanup function that restores cfgFile.
func tempEnv(t *testing.T, slugs ...string) func() {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	for _, slug := range slugs {
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
	return func() { cfgFile = old }
}

// buildRootCmd returns a fresh cobra command tree wired to the real runX functions.
func buildRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "marlin", SilenceUsage: true, SilenceErrors: true}

	add := &cobra.Command{Use: "add [registry-id]", Args: cobra.MaximumNArgs(1), RunE: runAdd}
	add.Flags().Bool("auto-detect", false, "")

	list := &cobra.Command{Use: "list", RunE: runList}

	sw := &cobra.Command{Use: "switch [model]", Args: cobra.MaximumNArgs(1), RunE: runSwitch}

	search := &cobra.Command{Use: "search <query>", Args: cobra.ExactArgs(1), RunE: runSearch}
	search.Flags().StringSlice("registry", []string{"huggingface", "ngc"}, "")

	validate := &cobra.Command{Use: "validate <model>", Args: cobra.ExactArgs(1), RunE: runValidate}

	status := &cobra.Command{Use: "status", RunE: runStatus}

	logs := &cobra.Command{Use: "logs", RunE: runLogs}
	logs.Flags().BoolP("follow", "f", false, "")
	logs.Flags().Int("lines", 100, "")

	root.AddCommand(add, list, sw, search, validate, status, logs)
	return root
}

func executeCmd(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root := buildRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// cmdWithContext returns a *cobra.Command backed by context.Background() and
// writing to buf. Useful for direct RunE calls that need cmd.Context().
func cmdWithContext(buf io.Writer) *cobra.Command {
	cmd := &cobra.Command{SilenceUsage: true}
	cmd.SetContext(context.Background())
	if buf != nil {
		cmd.SetOut(buf)
		cmd.SetErr(buf)
	}
	return cmd
}

// --- SetVersionInfo ---

func TestSetVersionInfo(t *testing.T) {
	old := rootCmd.Version
	defer func() { rootCmd.Version = old }()
	SetVersionInfo("1.2.3", "abc123", "2024-01-01")
	assert.Equal(t, "1.2.3 (commit: abc123, built: 2024-01-01)", rootCmd.Version)
}

// --- Execute() and initConfig() ---

func TestExecuteSuccess(t *testing.T) {
	old := osExit
	osExit = func(int) { t.Fatal("os.Exit called unexpectedly") }
	defer func() { osExit = old }()

	rootCmd.SetArgs([]string{"--help"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	Execute()
	rootCmd.SetArgs(nil)
}

func TestExecuteError(t *testing.T) {
	var exitCode int
	old := osExit
	osExit = func(code int) { exitCode = code }
	defer func() { osExit = old }()

	rootCmd.SetArgs([]string{"unknown-xyz-command"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	Execute()
	rootCmd.SetArgs(nil)
	assert.Equal(t, 1, exitCode)
}

func TestInitConfigWithFile(t *testing.T) {
	f, err := os.CreateTemp("", "marlin-*.toml")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, _ = f.WriteString("[behavior]\nswitch_prompt = true\n")
	f.Close()

	old := cfgFile
	cfgFile = f.Name()
	defer func() { cfgFile = old }()

	viper.Reset()
	initConfig()
}

func TestInitConfigBadFile(t *testing.T) {
	var exitCode int
	old := osExit
	osExit = func(code int) { exitCode = code }
	defer func() { osExit = old }()

	f, err := os.CreateTemp("", "marlin-bad-*.toml")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, _ = f.WriteString("[[[[invalid toml")
	f.Close()

	old2 := cfgFile
	cfgFile = f.Name()
	defer func() { cfgFile = old2 }()

	viper.Reset()
	initConfig()
	assert.Equal(t, 1, exitCode)
}

// --- Command routing via fresh tree ---

func TestRootHelp(t *testing.T) {
	_, err := executeCmd("--help")
	require.NoError(t, err)
}

func TestAddCmd(t *testing.T) {
	// Wizard fails gracefully (no TTY) and returns nil.
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("add", "Qwen/Qwen2.5-72B-Instruct-AWQ")
	require.NoError(t, err)
}

func TestAddCmdNoArg(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("add")
	require.NoError(t, err)
}

func TestAddCmdTooManyArgs(t *testing.T) {
	_, err := executeCmd("add", "a", "b")
	assert.Error(t, err)
}

func TestRunAddSuccessful(t *testing.T) {
	old := runAddWizardFunc
	runAddWizardFunc = func() (*ui.WizardResult, error) {
		return &ui.WizardResult{
			Slug: "new-model",
			Cfg: &config.ModelConfig{
				Model: config.ModelMeta{
					Type:   config.ProviderVLLM,
					ID:     "test/new-model",
					Status: config.StatusUntested,
				},
				Serve: config.ServeConfig{GPUMemoryUtilization: 0.9},
			},
		}, nil
	}
	defer func() { runAddWizardFunc = old }()

	cleanup := tempEnv(t)
	defer cleanup()

	out, err := executeCmd("add")
	require.NoError(t, err)
	assert.Contains(t, out, "created")
	assert.Contains(t, out, "new-model")
}

func TestRunAddFileAlreadyExists(t *testing.T) {
	old := runAddWizardFunc
	runAddWizardFunc = func() (*ui.WizardResult, error) {
		return &ui.WizardResult{
			Slug: "qwen25-72b",
			Cfg: &config.ModelConfig{
				Model: config.ModelMeta{Type: config.ProviderVLLM, ID: "test/model", Status: config.StatusUntested},
				Serve: config.ServeConfig{GPUMemoryUtilization: 0.9},
			},
		}, nil
	}
	defer func() { runAddWizardFunc = old }()

	// tempEnv with "qwen25-72b" already written.
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()

	_, err := executeCmd("add")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestListCmdEmpty(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	out, err := executeCmd("list")
	require.NoError(t, err)
	assert.Contains(t, out, "no models found")
}

func TestListCmdWithModels(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b", "llama-8b")
	defer cleanup()
	out, err := executeCmd("list")
	require.NoError(t, err)
	assert.Contains(t, out, "qwen25-72b")
	assert.Contains(t, out, "llama-8b")
}

func TestSwitchCmdModelNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("switch", "nonexistent")
	assert.Error(t, err)
}

func TestSwitchCmdMissingArg(t *testing.T) {
	// No models in dir → picker can't launch in CI, so error expected.
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("switch")
	assert.Error(t, err)
}

func TestSearchCmd(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	// Network calls will fail in CI; the command handles them as warnings.
	_, err := executeCmd("search", "llama")
	require.NoError(t, err)
}

func TestSearchCmdMissingArg(t *testing.T) {
	_, err := executeCmd("search")
	assert.Error(t, err)
}

func TestSearchCmdRegistryFlag(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("search", "--registry", "ngc", "llama")
	require.NoError(t, err)
}

func TestValidateCmdModelNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("validate", "nonexistent")
	assert.Error(t, err)
}

func TestValidateCmdWithModel(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	out, err := executeCmd("validate", "qwen25-72b")
	require.NoError(t, err)
	// Model has served_model_name = ["gn100"] matching alias, no gpu warn → OK
	assert.Contains(t, out, "OK")
}

func TestValidateCmdMissingArg(t *testing.T) {
	_, err := executeCmd("validate")
	assert.Error(t, err)
}

func TestStatusCmdNoActiveModel(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	out, err := executeCmd("status")
	require.NoError(t, err)
	assert.Contains(t, out, "no active model")
}

func TestLogsCmd(t *testing.T) {
	restore := provider.SetRunCommandForTest(func(_ context.Context, w io.Writer, _ string, _ ...string) error {
		fmt.Fprintln(w, "fake log line")
		return nil
	})
	defer restore()

	cleanup := tempEnv(t)
	defer cleanup()
	out, err := executeCmd("logs")
	require.NoError(t, err)
	assert.Contains(t, out, "fake log line")
}

func TestLogsCmdFollowFlag(t *testing.T) {
	restore := provider.SetRunCommandForTest(func(_ context.Context, w io.Writer, _ string, _ ...string) error {
		fmt.Fprintln(w, "following")
		return nil
	})
	defer restore()

	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("logs", "--follow")
	require.NoError(t, err)
}

func TestLogsCmdLinesFlag(t *testing.T) {
	restore := provider.SetRunCommandForTest(func(_ context.Context, w io.Writer, _ string, _ ...string) error {
		return nil
	})
	defer restore()

	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("logs", "--lines", "50")
	require.NoError(t, err)
}

// --- Direct RunE calls with proper context ---

func TestRunAddDirect(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	require.NoError(t, runAdd(cmdWithContext(io.Discard), []string{"some/model"}))
}

func TestRunListDirect(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	require.NoError(t, runList(cmdWithContext(io.Discard), nil))
}

func TestRunSwitchDirectNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	err := runSwitch(cmdWithContext(io.Discard), []string{"nonexistent"})
	assert.Error(t, err)
}

func TestRunSearchDirect(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cmd := cmdWithContext(io.Discard)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	require.NoError(t, runSearch(cmd, []string{"llama"}))
}

func TestRunValidateDirect(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	require.NoError(t, runValidate(cmdWithContext(io.Discard), []string{"qwen25-72b"}))
}

func TestRunStatusDirect(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	require.NoError(t, runStatus(cmdWithContext(io.Discard), nil))
}

func TestRunLogsDirect(t *testing.T) {
	restore := provider.SetRunCommandForTest(func(_ context.Context, w io.Writer, _ string, _ ...string) error {
		return nil
	})
	defer restore()

	cleanup := tempEnv(t)
	defer cleanup()

	cmd := cmdWithContext(io.Discard)
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().Int("lines", 100, "")
	require.NoError(t, runLogs(cmd, nil))
}
