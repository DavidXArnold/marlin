package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

func TestRmSingleArg(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b", "llama-8b")
	defer cleanup()

	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { rmConfirmFunc = old }()

	_, err := executeCmd("rm", "qwen25-72b")
	require.NoError(t, err)
}

func TestRmMultipleArgs(t *testing.T) {
	cleanup := tempEnv(t, "alpha", "beta", "gamma")
	defer cleanup()

	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { rmConfirmFunc = old }()

	_, err := executeCmd("rm", "alpha", "gamma")
	require.NoError(t, err)
}

func TestRmArgNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	_, err := executeCmd("rm", "ghost-model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRmMultiSelect(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b", "llama-8b")
	defer cleanup()

	oldMulti := rmMultiPickFunc
	rmMultiPickFunc = func(_ []string, _ []*config.ModelConfig, _ string, _ map[string]time.Time) ([]string, error) {
		return []string{"llama-8b"}, nil
	}
	defer func() { rmMultiPickFunc = oldMulti }()

	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { rmConfirmFunc = old }()

	_, err := executeCmd("rm")
	require.NoError(t, err)
}

func TestRmCancel(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()

	old := rmConfirmFunc
	rmConfirmFunc = func(string) (bool, error) { return false, nil }
	defer func() { rmConfirmFunc = old }()

	out, err := executeCmd("rm", "qwen25-72b")
	require.NoError(t, err)
	assert.Contains(t, out, "cancelled")
}

func TestRmNothingSelected(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()

	oldMulti := rmMultiPickFunc
	rmMultiPickFunc = func(_ []string, _ []*config.ModelConfig, _ string, _ map[string]time.Time) ([]string, error) {
		return nil, nil
	}
	defer func() { rmMultiPickFunc = oldMulti }()

	out, err := executeCmd("rm")
	require.NoError(t, err)
	assert.Contains(t, out, "nothing selected")
}

func TestRmBundledWarning(t *testing.T) {
	cleanup := tempEnv(t, "nvfp4-base", "llama-8b")
	defer cleanup()

	oldBundled := isBundledFunc
	isBundledFunc = func(slug string) bool { return slug == "nvfp4-base" }
	defer func() { isBundledFunc = oldBundled }()

	old := rmConfirmFunc
	var promptSeen string
	rmConfirmFunc = func(p string) (bool, error) {
		promptSeen = p
		return false, nil
	}
	defer func() { rmConfirmFunc = old }()

	out, err := executeCmd("rm", "nvfp4-base")
	require.NoError(t, err)
	assert.Contains(t, out, "bundled")
	assert.Contains(t, promptSeen, "nvfp4-base")
}
