//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMarlin executes the compiled marlin binary with --config pointing at env's
// config file, appends any extra args, and returns combined stdout+stderr and exit code.
func runMarlin(t *testing.T, env *testEnv, args ...string) (string, int) {
	t.Helper()
	cmdArgs := append([]string{"--config", env.cfgPath}, args...)
	cmd := exec.Command(testBinary, cmdArgs...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	return string(out), code
}

func TestE2EList(t *testing.T) {
	env := newTestEnv(t)
	env.addModel(t, "qwen25-72b-awq")
	env.addModel(t, "llama-3-8b")

	out, code := runMarlin(t, env, "list")
	require.Equal(t, 0, code, "list should exit 0\noutput:\n%s", out)
	assert.Contains(t, out, "qwen25-72b-awq")
	assert.Contains(t, out, "llama-3-8b")
	assert.Contains(t, out, "vllm")
	assert.Contains(t, out, "working")
}

func TestE2EListEmpty(t *testing.T) {
	env := newTestEnv(t)
	out, code := runMarlin(t, env, "list")
	require.Equal(t, 0, code, "list with no models should exit 0\noutput:\n%s", out)
	assert.Contains(t, out, "no models found")
}

func TestE2EValidateOK(t *testing.T) {
	env := newTestEnv(t)
	env.addModel(t, "qwen25-72b-awq")

	out, code := runMarlin(t, env, "validate", "qwen25-72b-awq")
	require.Equal(t, 0, code, "validate of well-formed model should exit 0\noutput:\n%s", out)
	assert.Contains(t, out, "OK")
}

func TestE2EValidateMissing(t *testing.T) {
	env := newTestEnv(t)
	out, code := runMarlin(t, env, "validate", "nonexistent-model")
	assert.NotEqual(t, 0, code, "validate of missing model should exit nonzero\noutput:\n%s", out)
}

func TestE2EVersion(t *testing.T) {
	cmd := exec.Command(testBinary, "--help")
	out, err := cmd.CombinedOutput()
	// --help exits 0 for cobra commands
	assert.NoError(t, err, "help output:\n%s", out)
	assert.Contains(t, string(out), "marlin")
}

// TestE2EStatus requires a real vLLM server and is skipped in CI.
func TestE2EStatus(t *testing.T) {
	if os.Getenv("MARLIN_TEST_HOST") == "" {
		t.Skip("MARLIN_TEST_HOST not set — skipping live status test")
	}
	env := newTestEnv(t)
	out, code := runMarlin(t, env, "status")
	assert.Equal(t, 0, code, "status should exit 0\noutput:\n%s", out)
}
