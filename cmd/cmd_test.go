package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/registry"
	"github.com/DavidXArnold/marlin/internal/secrets"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/internal/state"
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
	search.Flags().Bool("plain", false, "")

	validate := &cobra.Command{Use: "validate <model>", Args: cobra.ExactArgs(1), RunE: runValidate}

	status := &cobra.Command{Use: "status", RunE: runStatus}

	logs := &cobra.Command{Use: "logs", RunE: runLogs}
	logs.Flags().BoolP("follow", "f", false, "")
	logs.Flags().Int("lines", 100, "")

	run := &cobra.Command{Use: "run <model>", Args: cobra.ExactArgs(1), RunE: runRun}
	run.Flags().BoolP("detach", "d", false, "")

	ps := &cobra.Command{Use: "ps", RunE: runPs}

	stop := &cobra.Command{Use: "stop [model]", Args: cobra.MaximumNArgs(1), RunE: runStop}

	rm := &cobra.Command{Use: "rm <model>", Args: cobra.ExactArgs(1), RunE: runRm}

	edit := &cobra.Command{Use: "edit <model>", Args: cobra.ExactArgs(1), RunE: runEdit}

	completion := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE:      completionCmd.RunE,
	}

	configure := &cobra.Command{Use: "configure", Args: cobra.NoArgs, RunE: runConfigure}

	start := &cobra.Command{Use: "start [model]", Args: cobra.MaximumNArgs(1), RunE: runStart}
	start.Flags().Bool("enable", false, "")

	root.AddCommand(add, list, sw, search, validate, status, logs, run, ps, stop, rm, edit, completion, configure, start)
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

type fakeRegistry struct {
	name    string
	results []registry.ModelInfo
	err     error
}

func (f fakeRegistry) Name() string { return f.name }

func (f fakeRegistry) Search(context.Context, string) ([]registry.ModelInfo, error) {
	return f.results, f.err
}

func (f fakeRegistry) Fetch(context.Context, string) (*registry.ModelInfo, error) {
	return nil, fmt.Errorf("not implemented")
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
	defer func() { _ = os.Remove(f.Name()) }()
	_, _ = f.WriteString("[behavior]\nswitch_prompt = true\n")
	require.NoError(t, f.Close())

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
	defer func() { _ = os.Remove(f.Name()) }()
	_, _ = f.WriteString("[[[[invalid toml")
	require.NoError(t, f.Close())

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
	// Model has served_model_name = ["gn100"] matching alias set in tempEnv → OK
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
		_, _ = fmt.Fprintln(w, "fake log line")
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
		_, _ = fmt.Fprintln(w, "following")
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

func TestRunListMarksActiveModel(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "qwen25-72b",
		ActiveProvider: config.ProviderVLLM,
	}))

	out, err := executeCmd("list")
	require.NoError(t, err)
	assert.Contains(t, out, "qwen25-72b")
	assert.Contains(t, out, "active")
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
	cmd.Flags().Bool("plain", false, "")
	require.NoError(t, runSearch(cmd, []string{"llama"}))
}

func TestRunSearchDiscoveryNoArgs(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	oldHF := newHuggingFaceRegistry
	newHuggingFaceRegistry = func(string) registry.Registry {
		return fakeRegistry{
			name: "huggingface",
			results: []registry.ModelInfo{
				{ID: "meta-llama/Llama-3.1-8B-Instruct", Registry: "huggingface", ParamsBillion: 8},
			},
		}
	}
	defer func() { newHuggingFaceRegistry = oldHF }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", true, "")

	require.NoError(t, runSearch(cmd, nil)) // no args = discovery mode
	assert.Contains(t, buf.String(), "meta-llama")
}

func TestRunSearchPlainTableWithResults(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	oldHF := newHuggingFaceRegistry
	newHuggingFaceRegistry = func(string) registry.Registry {
		return fakeRegistry{
			name: "huggingface",
			results: []registry.ModelInfo{
				{
					ID:            "meta-llama/Llama-3.1-8B-Instruct",
					Registry:      "huggingface",
					Description:   "chat model",
					ParamsBillion: 8,
					Quantization:  "awq",
				},
			},
		}
	}
	defer func() { newHuggingFaceRegistry = oldHF }()

	cmd := cmdWithContext(new(bytes.Buffer))
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", true, "")

	require.NoError(t, runSearch(cmd, []string{"llama"}))
	out := cmd.OutOrStdout().(*bytes.Buffer).String()
	assert.Contains(t, out, "[huggingface]")
	assert.Contains(t, out, "meta-llama/Llama-3.1-8B-Instruct")
	assert.Contains(t, out, "VRAM EST")
}

func TestRunSearchWarnsOnRegistryError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	oldHF := newHuggingFaceRegistry
	newHuggingFaceRegistry = func(string) registry.Registry {
		return fakeRegistry{name: "huggingface", err: fmt.Errorf("offline")}
	}
	defer func() { newHuggingFaceRegistry = oldHF }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", true, "")

	require.NoError(t, runSearch(cmd, []string{"llama"}))
	assert.Contains(t, buf.String(), "warning: huggingface search failed: offline")
}

func TestRunSearchPlainNoResults(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	oldHF := newHuggingFaceRegistry
	newHuggingFaceRegistry = func(string) registry.Registry {
		return fakeRegistry{name: "huggingface"}
	}
	defer func() { newHuggingFaceRegistry = oldHF }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", true, "")

	require.NoError(t, runSearch(cmd, []string{"llama"}))
	assert.Contains(t, buf.String(), "[huggingface] no results")
}

func TestRunSearchNGCWithConfiguredKey(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, secrets.Save(cfg.Paths.SecretsEnv, map[string]string{"NGC_API_KEY": "nvapi_test"}))

	oldNGC := newNGCRegistry
	newNGCRegistry = func(apiKey string) registry.Registry {
		assert.Equal(t, "nvapi_test", apiKey)
		return fakeRegistry{
			name: "ngc",
			results: []registry.ModelInfo{
				{ID: "nvcr.io/nim/meta/llama:latest", Registry: "ngc", Description: "nim"},
			},
		}
	}
	defer func() { newNGCRegistry = oldNGC }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"ngc"}, "")
	cmd.Flags().Bool("plain", true, "")

	require.NoError(t, runSearch(cmd, []string{"llama"}))
	assert.Contains(t, buf.String(), "[ngc]")
	assert.Contains(t, buf.String(), "meta/llama")
}

func TestRunSearchInteractiveBrowseOpensURL(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	selected := registry.ModelInfo{ID: "Qwen/Qwen2.5-7B", Registry: "huggingface"}
	restoreSearchUI(t, &selected, ui.SearchActionBrowse, "https://huggingface.co/Qwen/Qwen2.5-7B")
	restoreSingleHFResult(t, selected)

	oldTerminal := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	defer func() { stdoutIsTerminal = oldTerminal }()

	var opened string
	oldOpen := openBrowserCmd
	openBrowserCmd = func(url string) error {
		opened = url
		return nil
	}
	defer func() { openBrowserCmd = oldOpen }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", false, "")

	require.NoError(t, runSearch(cmd, []string{"qwen"}))
	assert.Equal(t, "https://huggingface.co/Qwen/Qwen2.5-7B", opened)
	assert.Contains(t, buf.String(), "opening https://huggingface.co/Qwen/Qwen2.5-7B")
}

func TestRunSearchInteractiveBrowseNoURL(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	selected := registry.ModelInfo{ID: "unknown/model", Registry: "unknown"}
	restoreSearchUI(t, &selected, ui.SearchActionBrowse, "")
	restoreSingleHFResult(t, selected)

	oldTerminal := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	defer func() { stdoutIsTerminal = oldTerminal }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", false, "")

	require.NoError(t, runSearch(cmd, []string{"unknown"}))
	assert.Contains(t, buf.String(), "no URL available")
}

func TestRunSearchInteractiveAdd(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	selected := registry.ModelInfo{ID: "Qwen/Qwen2.5-7B", Registry: "huggingface"}
	restoreSearchUI(t, &selected, ui.SearchActionAdd, "https://huggingface.co/Qwen/Qwen2.5-7B")
	restoreSingleHFResult(t, selected)

	oldTerminal := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	defer func() { stdoutIsTerminal = oldTerminal }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", false, "")

	require.NoError(t, runSearch(cmd, []string{"qwen"}))
	assert.Contains(t, buf.String(), "created")
}

func restoreSearchUI(t *testing.T, selected *registry.ModelInfo, action ui.SearchAction, url string) {
	t.Helper()
	oldPick := pickSearchResult
	oldAction := searchActionMenu
	oldURL := modelURL
	pickSearchResult = func([]registry.ModelInfo, uint64) (*registry.ModelInfo, error) {
		return selected, nil
	}
	searchActionMenu = func(string, string) (ui.SearchAction, error) {
		return action, nil
	}
	modelURL = func(registry.ModelInfo) string {
		return url
	}
	t.Cleanup(func() {
		pickSearchResult = oldPick
		searchActionMenu = oldAction
		modelURL = oldURL
	})
}

func restoreSingleHFResult(t *testing.T, result registry.ModelInfo) {
	t.Helper()
	oldHF := newHuggingFaceRegistry
	newHuggingFaceRegistry = func(string) registry.Registry {
		return fakeRegistry{name: "huggingface", results: []registry.ModelInfo{result}}
	}
	t.Cleanup(func() { newHuggingFaceRegistry = oldHF })
}

func TestAddFromSearchResultNew(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	m := registry.ModelInfo{
		ID:            "Qwen/Qwen2.5-7B-Instruct-AWQ",
		Registry:      "huggingface",
		Quantization:  "awq",
		ParamsBillion: 7,
	}
	err = addFromSearchResult(cfg, m, io.Discard, false)
	require.NoError(t, err)
}

func TestAddFromSearchResultAlreadyExists(t *testing.T) {
	// AutoSlug("Qwen/Qwen2.5-7B-Instruct-AWQ") → "qwen2.5-7b-instruct-awq"
	cleanup := tempEnv(t, "qwen2.5-7b-instruct-awq")
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	m := registry.ModelInfo{ID: "Qwen/Qwen2.5-7B-Instruct-AWQ", Registry: "huggingface"}
	err = addFromSearchResult(cfg, m, io.Discard, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestAddFromSearchResultNGC(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)

	m := registry.ModelInfo{ID: "nvcr.io/nim/meta/llama:latest", Registry: "ngc"}
	require.NoError(t, addFromSearchResult(cfg, m, io.Discard, false))

	slug := ui.AutoSlug(m.ID)
	saved, err := config.LoadModel(filepath.Join(cfg.Paths.ModelsDir, slug+".toml"))
	require.NoError(t, err)
	assert.Equal(t, config.ProviderNIM, saved.Model.Type)
	assert.Equal(t, "nvcr.io/nim/meta/llama:latest", saved.Model.Image)
}

// --- configure ---

func TestRunConfigureNoChanges(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := configureIn
	configureIn = strings.NewReader("\n\n") // Enter for each prompt → keep/skip
	defer func() { configureIn = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runConfigure(cmd, nil))
	assert.Contains(t, buf.String(), "No changes made")
}

func TestRunConfigureSetsToken(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := configureIn
	configureIn = strings.NewReader("hf_testtoken\n\n") // HF_TOKEN then skip NGC
	defer func() { configureIn = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runConfigure(cmd, nil))
	assert.Contains(t, buf.String(), "Saved to")

	cfg, err := globalConfig()
	require.NoError(t, err)
	m, err := secrets.Load(cfg.Paths.SecretsEnv)
	require.NoError(t, err)
	assert.Equal(t, "hf_testtoken", m["HF_TOKEN"])
}

func TestRunConfigureShowsURLs(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := configureIn
	configureIn = strings.NewReader("\n\n")
	defer func() { configureIn = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runConfigure(cmd, nil))
	assert.Contains(t, buf.String(), "huggingface.co/settings/tokens")
	assert.Contains(t, buf.String(), "ngc.nvidia.com/setup/personal-keys")
}

func TestRunConfigureKeepsExistingSecrets(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, secrets.Save(cfg.Paths.SecretsEnv, map[string]string{
		"HF_TOKEN":    "hf_existing",
		"NGC_API_KEY": "nvapi_existing",
	}))

	old := configureIn
	configureIn = strings.NewReader("\n\n")
	defer func() { configureIn = old }()

	var buf bytes.Buffer
	require.NoError(t, runConfigure(cmdWithContext(&buf), nil))
	assert.Contains(t, buf.String(), "Status:   [set]")
	assert.Contains(t, buf.String(), "No changes made")

	got, err := secrets.Load(cfg.Paths.SecretsEnv)
	require.NoError(t, err)
	assert.Equal(t, "hf_existing", got["HF_TOKEN"])
	assert.Equal(t, "nvapi_existing", got["NGC_API_KEY"])
}

func TestRunConfigureNGCDockerLoginSuccess(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	oldIn := configureIn
	configureIn = strings.NewReader("\nnvapi_test\ny\n")
	defer func() { configureIn = oldIn }()

	called := false
	oldLogin := dockerLoginFunc
	dockerLoginFunc = func(apiKey string) error {
		called = true
		assert.Equal(t, "nvapi_test", apiKey)
		return nil
	}
	defer func() { dockerLoginFunc = oldLogin }()

	var buf bytes.Buffer
	require.NoError(t, runConfigure(cmdWithContext(&buf), nil))
	assert.True(t, called)
	assert.Contains(t, buf.String(), "Docker authenticated")
}

func TestRunConfigureNGCDockerLoginFailure(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	oldIn := configureIn
	configureIn = strings.NewReader("\nnvapi_test\ny\n")
	defer func() { configureIn = oldIn }()

	oldLogin := dockerLoginFunc
	dockerLoginFunc = func(string) error { return fmt.Errorf("denied") }
	defer func() { dockerLoginFunc = oldLogin }()

	var buf bytes.Buffer
	require.NoError(t, runConfigure(cmdWithContext(&buf), nil))
	assert.Contains(t, buf.String(), "docker login failed")
	assert.Contains(t, buf.String(), "Run it manually")
}

// --- buildRegistries ---

func TestBuildRegistriesSkipsNGCWithNoKey(t *testing.T) {
	regs := buildRegistries([]string{"huggingface", "ngc"}, map[string]string{})
	require.Len(t, regs, 1)
	assert.Equal(t, "huggingface", regs[0].Name())
}

func TestBuildRegistriesIncludesNGCWhenKeySet(t *testing.T) {
	regs := buildRegistries([]string{"ngc"}, map[string]string{"NGC_API_KEY": "nvapi-test"})
	require.Len(t, regs, 1)
	assert.Equal(t, "ngc", regs[0].Name())
}

// --- search helpers ---

func TestFormatUpdated(t *testing.T) {
	assert.Equal(t, "-", ui.FormatUpdated(time.Time{}))
	assert.Equal(t, "today", ui.FormatUpdated(time.Now()))
	assert.Contains(t, ui.FormatUpdated(time.Now().AddDate(0, 0, -3)), "d ago")
	assert.Contains(t, ui.FormatUpdated(time.Now().AddDate(0, 0, -14)), "w ago")
	assert.Contains(t, ui.FormatUpdated(time.Now().AddDate(0, -2, 0)), "mo ago")
	assert.Contains(t, ui.FormatUpdated(time.Now().AddDate(-2, 0, 0)), "y ago")
}

func TestFitLabel(t *testing.T) {
	assert.Equal(t, "?", ui.FitLabel(0, 1000))
	assert.Equal(t, "?", ui.FitLabel(1000, 0))
	assert.Equal(t, "✓", ui.FitLabel(800, 1000))
	assert.Equal(t, "~", ui.FitLabel(900, 1000))
	assert.Equal(t, "✗", ui.FitLabel(1100, 1000))
}

func TestFormatVRAM(t *testing.T) {
	assert.Equal(t, "-", formatVRAM(0))
	assert.NotEmpty(t, formatVRAM(8192))
}

func TestModelConfigFromInfoNGC(t *testing.T) {
	m := registry.ModelInfo{
		ID:       "nvcr.io/nim/meta/llama:latest",
		Registry: "ngc",
	}
	mc := modelConfigFromInfo(m, "local")
	assert.Equal(t, config.ProviderNIM, mc.Model.Type)
	assert.Equal(t, "nvcr.io/nim/meta/llama:latest", mc.Model.Image)
	assert.Empty(t, mc.Serve.ServedModelName)
}

func TestModelConfigFromInfoHuggingFace(t *testing.T) {
	m := registry.ModelInfo{
		ID:           "Qwen/Qwen2.5-7B",
		Registry:     "huggingface",
		Quantization: "awq",
	}
	mc := modelConfigFromInfo(m, "local")
	assert.Equal(t, config.ProviderVLLM, mc.Model.Type)
	assert.Equal(t, "Qwen/Qwen2.5-7B", mc.Model.ID)
	assert.Equal(t, []string{"local"}, mc.Serve.ServedModelName)
	assert.Equal(t, "awq", mc.Serve.Quantization)
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

func TestRunStatusActiveModelReady(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "qwen25-72b.toml"), []byte(`[model]
id = "qwen-model-id"
type = "vllm"
status = "untested"
`), 0o644))

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	srv.Listener = listener
	srv.Start()
	defer srv.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	cfgPath := filepath.Join(dir, "config.toml")
	statePath := filepath.Join(dir, "state.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`[behavior]
warn_unmanaged_containers = false

[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q

[server]
host = "127.0.0.1"
port = %d
alias = "gn100"
`, modelsDir, statePath, filepath.Join(dir, "secrets.env"), filepath.Join(dir, "model.env"), port)), 0o644))

	old := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = old }()

	require.NoError(t, state.Save(statePath, &state.State{
		ActiveModel:    "qwen25-72b",
		ActiveProvider: config.ProviderVLLM,
		ContainerID:    "abcdef1234567890",
	}))

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "active model : qwen25-72b")
	assert.Contains(t, out, "provider     : vllm")
	assert.Contains(t, out, "container    : abcdef123456")
	assert.Contains(t, out, "api health   : ready")
}

func injectBootStatus(t *testing.T, enabled bool) {
	t.Helper()
	old := newStatusSystemdManager
	newStatusSystemdManager = func(unit string) *service.SystemdManager {
		runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			if enabled {
				return []byte(""), nil
			}
			return []byte("disabled"), &disabledErr{}
		}
		return service.NewSystemdManagerWithRunner(unit, runner)
	}
	t.Cleanup(func() { newStatusSystemdManager = old })
}

// disabledErr mimics systemctl exit code 1 (unit disabled).
type disabledErr struct{}

func (e *disabledErr) Error() string { return "exit status 1" }
func (e *disabledErr) ExitCode() int { return 1 }

func TestRunStatusBootEnabled(t *testing.T) {
	cleanup := tempEnv(t, "llama-8b")
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectBootStatus(t, true)

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	assert.Contains(t, buf.String(), "boot         : enabled")
}

func TestRunStatusBootDisabled(t *testing.T) {
	cleanup := tempEnv(t, "llama-8b")
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NoError(t, state.Save(cfg.Paths.StateFile, &state.State{
		ActiveModel:    "llama-8b",
		ActiveProvider: config.ProviderVLLM,
	}))

	injectBootStatus(t, false)

	var buf bytes.Buffer
	require.NoError(t, runStatus(cmdWithContext(&buf), nil))
	assert.Contains(t, buf.String(), "marlin start --enable")
}

func TestDiskLabel(t *testing.T) {
	assert.Equal(t, "(models)", diskLabel("/models", "/models", "/nim"))
	assert.Equal(t, "(nim cache)", diskLabel("/nim", "/models", "/nim"))
	assert.Empty(t, diskLabel("/other", "/models", "/nim"))
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

// --- completion ---

func TestCompletionBash(t *testing.T) {
	out, err := executeCmd("completion", "bash")
	require.NoError(t, err)
	assert.Contains(t, out, "bash")
}

func TestCompletionZsh(t *testing.T) {
	out, err := executeCmd("completion", "zsh")
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestCompletionFish(t *testing.T) {
	out, err := executeCmd("completion", "fish")
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestCompletionInvalidShell(t *testing.T) {
	_, err := executeCmd("completion", "invalid")
	assert.Error(t, err)
}

func TestCompletionNoArgs(t *testing.T) {
	_, err := executeCmd("completion")
	assert.Error(t, err)
}

// --- rm ---

func TestRmNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("rm", "nonexistent")
	assert.Error(t, err)
}

func TestRmExistingModel(t *testing.T) {
	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { rmConfirmFunc = old }()

	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	out, err := executeCmd("rm", "qwen25-72b")
	require.NoError(t, err)
	assert.Contains(t, out, "removed")
}

func TestRmCancelled(t *testing.T) {
	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return false, nil }
	defer func() { rmConfirmFunc = old }()

	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	var buf bytes.Buffer
	require.NoError(t, runRm(cmdWithContext(&buf), []string{"qwen25-72b"}))
	assert.Contains(t, buf.String(), "cancelled")
}

func TestRmMissingArg(t *testing.T) {
	_, err := executeCmd("rm")
	assert.Error(t, err)
}

func TestRunRmDirect(t *testing.T) {
	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { rmConfirmFunc = old }()

	cleanup := tempEnv(t, "llama-8b")
	defer cleanup()
	require.NoError(t, runRm(cmdWithContext(io.Discard), []string{"llama-8b"}))
}

// --- edit ---

func TestEditNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	_, err := executeCmd("edit", "nonexistent")
	assert.Error(t, err)
}

func TestEditExistingModel(t *testing.T) {
	old := execEditorFunc
	execEditorFunc = func(_, _ string) error { return nil }
	defer func() { execEditorFunc = old }()

	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	_, err := executeCmd("edit", "qwen25-72b")
	require.NoError(t, err)
}

func TestEditMissingArg(t *testing.T) {
	_, err := executeCmd("edit")
	assert.Error(t, err)
}

func TestRunEditDirect(t *testing.T) {
	old := execEditorFunc
	execEditorFunc = func(_, _ string) error { return nil }
	defer func() { execEditorFunc = old }()

	cleanup := tempEnv(t, "llama-8b")
	defer cleanup()
	require.NoError(t, runEdit(cmdWithContext(io.Discard), []string{"llama-8b"}))
}

// --- update notice in Execute() ---

func TestExecuteUpdateNotice(t *testing.T) {
	// Drain any leftover from previous tests.
	select {
	case <-updateNoticeCh:
	default:
	}

	oldVersion := currentVersion
	currentVersion = "0.0.1"
	defer func() { currentVersion = oldVersion }()

	// Pre-populate the channel so Execute() prints the notice.
	updateNoticeCh <- "v99.0.0"

	// Capture stderr.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	oldExit := osExit
	osExit = func(int) {}
	defer func() { osExit = oldExit }()

	rootCmd.SetArgs([]string{"--help"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	Execute()
	rootCmd.SetArgs(nil)

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "v99.0.0")
	assert.Contains(t, buf.String(), "0.0.1")
}

func TestPersistentPreRunE_UpdateEnabled(t *testing.T) {
	// Drain channel.
	select {
	case <-updateNoticeCh:
	default:
	}

	done := make(chan struct{})
	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		close(done)
		return "v99.0.0", true, nil
	}
	defer func() { checkForUpdate = old }()

	cleanup := tempEnv(t)
	defer cleanup()

	oldVersion := currentVersion
	currentVersion = "0.0.1"
	defer func() { currentVersion = oldVersion }()

	require.NoError(t, rootCmd.PersistentPreRunE(nil, nil))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("checkForUpdate goroutine not called")
	}
	// Give the goroutine time to send on the channel.
	time.Sleep(10 * time.Millisecond)
	select {
	case latest := <-updateNoticeCh:
		assert.Equal(t, "v99.0.0", latest)
	default:
		t.Fatal("update notice not sent to channel")
	}
}

func TestPersistentPreRunE_UpdateDisabled(t *testing.T) {
	called := false
	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		called = true
		return "", false, nil
	}
	defer func() { checkForUpdate = old }()

	cleanup := tempEnvWithBehavior(t, "check_updates = false")
	defer cleanup()

	require.NoError(t, rootCmd.PersistentPreRunE(nil, nil))
	time.Sleep(50 * time.Millisecond)
	assert.False(t, called, "checkForUpdate should not be called when check_updates=false")
}

// tempEnvWithBehavior creates a config with extra behavior lines appended.
func tempEnvWithBehavior(t *testing.T, behaviorLine string) func() {
	t.Helper()
	dir := t.TempDir()
	modelsDir := dir + "/models"
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	content := fmt.Sprintf(`[behavior]
%s

[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q
`, behaviorLine, modelsDir,
		dir+"/state.toml",
		dir+"/secrets.env",
		dir+"/model.env",
	)
	cfgPath := dir + "/config.toml"
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o644))

	old := cfgFile
	cfgFile = cfgPath
	return func() { cfgFile = old }
}
