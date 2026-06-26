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
)

func TestUpgradeAlreadyLatest(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		return "", false, nil
	}
	defer func() { checkForUpdate = old }()

	out, err := executeCmd("upgrade")
	require.NoError(t, err)
	assert.Contains(t, out, "up to date")
}

func TestUpgradeCancelled(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		return "v99.0.0", true, nil
	}
	defer func() { checkForUpdate = old }()

	oldConfirm := upgradeConfirmFunc
	upgradeConfirmFunc = func(string) (bool, error) { return false, nil }
	defer func() { upgradeConfirmFunc = oldConfirm }()

	out, err := executeCmd("upgrade")
	require.NoError(t, err)
	assert.Contains(t, out, "cancelled")
}

func TestUpgradeCheckError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		return "", false, fmt.Errorf("network error")
	}
	defer func() { checkForUpdate = old }()

	_, err := executeCmd("upgrade")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checking for updates")
}

func TestUpgradeHappyPath(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	// Fake exe: write a real file so EvalSymlinks works.
	exeDir := t.TempDir()
	fakeBin := filepath.Join(exeDir, "marlin")
	require.NoError(t, os.WriteFile(fakeBin, []byte("old"), 0o755))

	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		return "v99.0.0", true, nil
	}
	defer func() { checkForUpdate = old }()

	oldConfirm := upgradeConfirmFunc
	upgradeConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { upgradeConfirmFunc = oldConfirm }()

	oldExe := upgradeExeFunc
	upgradeExeFunc = func() (string, error) { return fakeBin, nil }
	defer func() { upgradeExeFunc = oldExe }()

	oldDownload := upgradeDownloadFunc
	upgradeDownloadFunc = func(_ context.Context, _, dest string) error {
		return os.WriteFile(dest, []byte("archive"), 0o644)
	}
	defer func() { upgradeDownloadFunc = oldDownload }()

	oldExtract := upgradeExtractFunc
	upgradeExtractFunc = func(_, _, dest string) error {
		return os.WriteFile(dest, []byte("new-binary"), 0o755)
	}
	defer func() { upgradeExtractFunc = oldExtract }()

	oldInstall := upgradeInstallFunc
	upgradeInstallFunc = func(_ io.Writer, src, dst string) (bool, error) {
		data, err := os.ReadFile(src)
		if err != nil {
			return false, err
		}
		return true, os.WriteFile(dst, data, 0o755)
	}
	defer func() { upgradeInstallFunc = oldInstall }()

	out, err := executeCmd("upgrade")
	require.NoError(t, err)
	assert.Contains(t, out, "v99.0.0")
	assert.Contains(t, out, "upgraded")

	got, err := os.ReadFile(fakeBin)
	require.NoError(t, err)
	assert.Equal(t, []byte("new-binary"), got)
}

func TestUpgradeDownloadError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	exeDir := t.TempDir()
	fakeBin := filepath.Join(exeDir, "marlin")
	require.NoError(t, os.WriteFile(fakeBin, []byte("old"), 0o755))

	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		return "v99.0.0", true, nil
	}
	defer func() { checkForUpdate = old }()

	oldConfirm := upgradeConfirmFunc
	upgradeConfirmFunc = func(string) (bool, error) { return true, nil }
	defer func() { upgradeConfirmFunc = oldConfirm }()

	oldExe := upgradeExeFunc
	upgradeExeFunc = func() (string, error) { return fakeBin, nil }
	defer func() { upgradeExeFunc = oldExe }()

	oldDownload := upgradeDownloadFunc
	upgradeDownloadFunc = func(_ context.Context, _, _ string) error {
		return fmt.Errorf("connection refused")
	}
	defer func() { upgradeDownloadFunc = oldDownload }()

	_, err := executeCmd("upgrade")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "downloading release")
}

func TestUpgradeAvailableShowsVersion(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := checkForUpdate
	checkForUpdate = func(_ context.Context, _ string) (string, bool, error) {
		return "v99.0.0", true, nil
	}
	defer func() { checkForUpdate = old }()

	oldConfirm := upgradeConfirmFunc
	upgradeConfirmFunc = func(prompt string) (bool, error) {
		assert.Contains(t, prompt, "v99.0.0")
		return false, nil
	}
	defer func() { upgradeConfirmFunc = oldConfirm }()

	out, err := executeCmd("upgrade")
	require.NoError(t, err)
	assert.Contains(t, out, "v99.0.0")
}
