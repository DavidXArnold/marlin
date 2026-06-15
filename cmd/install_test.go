package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/service"
)

// injectNoopSystemdManager replaces installSystemdManagerFunc with a no-op.
func injectNoopSystemdManager(t *testing.T) {
	t.Helper()
	old := installSystemdManagerFunc
	installSystemdManagerFunc = func(unit string) *service.SystemdManager {
		return service.NewSystemdManagerWithRunner(unit, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		})
	}
	t.Cleanup(func() { installSystemdManagerFunc = old })
}

// injectTempUnitPath makes installUnitPathFunc return a writable temp path.
func injectTempUnitPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "marlin.service")
	old := installUnitPathFunc
	installUnitPathFunc = func(_ *config.Config) string { return path }
	t.Cleanup(func() { installUnitPathFunc = old })
	return path
}

// installCmdWithContext returns a cobra.Command wired with install flags.
func installCmdWithContext(buf *bytes.Buffer) *cobra.Command {
	cmd := cmdWithContext(buf)
	cmd.Flags().Bool("enable", false, "")
	cmd.Flags().Bool("force", false, "")
	return cmd
}

// TestRunInstallWritesUnitFile: happy path — writes file and reloads daemon.
func TestRunInstallWritesUnitFile(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	injectNoopSystemdManager(t)
	unitPath := injectTempUnitPath(t)

	var buf bytes.Buffer
	cmd := installCmdWithContext(&buf)
	require.NoError(t, runInstall(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "wrote")
	assert.Contains(t, out, "daemon reloaded")
	assert.Contains(t, out, "marlin start")

	// Verify the file was actually written.
	data, err := os.ReadFile(unitPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[Unit]")
	assert.Contains(t, string(data), "vllm serve")
}

// TestRunInstallWithEnable: --enable also calls systemctl enable.
func TestRunInstallWithEnable(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	var enableCalled bool
	old := installSystemdManagerFunc
	installSystemdManagerFunc = func(unit string) *service.SystemdManager {
		return service.NewSystemdManagerWithRunner(unit, func(_ context.Context, name string, args ...string) ([]byte, error) {
			for _, a := range args {
				if a == "enable" {
					enableCalled = true
				}
			}
			return nil, nil
		})
	}
	t.Cleanup(func() { installSystemdManagerFunc = old })
	injectTempUnitPath(t)

	cmd := installCmdWithContext(new(bytes.Buffer))
	require.NoError(t, cmd.Flags().Set("enable", "true"))
	require.NoError(t, runInstall(cmd, nil))
	assert.True(t, enableCalled)
}

// TestRunInstallExistingFileNoForce: existing file + user says "n" → cancelled.
func TestRunInstallExistingFileNoForce(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	injectNoopSystemdManager(t)
	unitPath := injectTempUnitPath(t)

	// Pre-create the unit file so the overwrite check triggers.
	require.NoError(t, os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644))

	oldReader := installPromptReader
	installPromptReader = strings.NewReader("n\n")
	t.Cleanup(func() { installPromptReader = oldReader })

	var buf bytes.Buffer
	cmd := installCmdWithContext(&buf)
	require.NoError(t, runInstall(cmd, nil))
	assert.Contains(t, buf.String(), "cancelled")
}

// TestRunInstallExistingFileForce: existing file + --force → overwrites without prompt.
func TestRunInstallExistingFileForce(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	injectNoopSystemdManager(t)
	unitPath := injectTempUnitPath(t)
	require.NoError(t, os.WriteFile(unitPath, []byte("old content\n"), 0o644))

	var buf bytes.Buffer
	cmd := installCmdWithContext(&buf)
	require.NoError(t, cmd.Flags().Set("force", "true"))
	require.NoError(t, runInstall(cmd, nil))

	data, err := os.ReadFile(unitPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[Unit]")
	assert.NotContains(t, string(data), "old content")
}

// TestRunInstallHelp: --help does not error.
func TestRunInstallHelp(t *testing.T) {
	_, err := executeCmd("install", "--help")
	require.NoError(t, err)
}
