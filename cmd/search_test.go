package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/registry"
	"github.com/DavidXArnold/marlin/internal/ui"
)

// fakeHFResult returns a minimal HuggingFace ModelInfo for testing.
func fakeHFResult(id string) registry.ModelInfo {
	return registry.ModelInfo{ID: id, Registry: "huggingface", ParamsBillion: 7, Quantization: "awq"}
}

// — runFromSearchResult —

func TestRunFromSearchResultSuccess(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	stub := &stubAdhoc{}
	injectAdhocRunner(t, stub)

	cfg, err := globalConfig()
	require.NoError(t, err)

	var buf bytes.Buffer
	err = runFromSearchResult(cmdWithContext(&buf), cfg, fakeHFResult("Qwen/Qwen2.5-7B"), &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "running")
}

func TestRunFromSearchResultRunnerError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	stub := &stubAdhoc{foregroundErr: fmt.Errorf("GPU not available")}
	injectAdhocRunner(t, stub)

	cfg, err := globalConfig()
	require.NoError(t, err)

	var buf bytes.Buffer
	err = runFromSearchResult(cmdWithContext(&buf), cfg, fakeHFResult("org/model-7b"), &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GPU not available")
}

func TestRunFromSearchResultBuildRunnerError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	injectAdhocRunnerErr(t, fmt.Errorf("no container runtime"))

	cfg, err := globalConfig()
	require.NoError(t, err)

	var buf bytes.Buffer
	err = runFromSearchResult(cmdWithContext(&buf), cfg, fakeHFResult("org/model-7b"), &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no container runtime")
}

// — SearchActionRun end-to-end via runSearch —

func TestRunSearchInteractiveRun(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	selected := registry.ModelInfo{ID: "Qwen/Qwen2.5-7B", Registry: "huggingface", ParamsBillion: 7}
	restoreSearchUI(t, &selected, ui.SearchActionRun, "https://huggingface.co/Qwen/Qwen2.5-7B")
	restoreSingleHFResult(t, selected)

	stub := &stubAdhoc{}
	injectAdhocRunner(t, stub)

	oldTerminal := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	defer func() { stdoutIsTerminal = oldTerminal }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().StringSlice("registry", []string{"huggingface"}, "")
	cmd.Flags().Bool("plain", false, "")

	require.NoError(t, runSearch(cmd, []string{"qwen"}))
	assert.Contains(t, buf.String(), "running")
}
