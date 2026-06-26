package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/ui"
	"github.com/DavidXArnold/marlin/internal/update"
)

var (
	upgradeConfirmFunc  = ui.Confirm
	upgradeDownloadFunc = func(ctx context.Context, url, dest string) error {
		return update.Download(ctx, url, dest)
	}
	upgradeExtractFunc = func(archive, bin, dest string) error {
		return update.ExtractBinary(archive, bin, dest)
	}
	upgradeInstallFunc = privilege.PromptAndInstallBinary
	upgradeExeFunc     = os.Executable
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Download and install the latest version of marlin",
	Args:  cobra.NoArgs,
	RunE:  runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(w, "checking for updates...")
	latest, newer, err := checkForUpdate(ctx, currentVersion)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	if !newer {
		v := currentVersion
		if v == "" {
			v = "unknown"
		}
		_, _ = fmt.Fprintf(w, "marlin is up to date (%s)\n", v)
		return nil
	}

	if currentVersion != "" {
		_, _ = fmt.Fprintf(w, "marlin %s is available (running %s)\n", latest, currentVersion)
	} else {
		_, _ = fmt.Fprintf(w, "marlin %s is available\n", latest)
	}

	ok, err := upgradeConfirmFunc(fmt.Sprintf("Install %s?", latest))
	if err != nil {
		return err
	}
	if !ok {
		_, _ = fmt.Fprintln(w, "cancelled")
		return nil
	}

	exe, err := upgradeExeFunc()
	if err != nil {
		return fmt.Errorf("finding current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving binary symlink: %w", err)
	}

	assetURL := update.AssetURL(latest, runtime.GOOS, runtime.GOARCH)
	_, _ = fmt.Fprintf(w, "downloading %s...\n", assetURL)

	tmpArchive, err := os.CreateTemp("", "marlin-upgrade-*.tar.gz")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	archivePath := tmpArchive.Name()
	_ = tmpArchive.Close()
	defer func() { _ = os.Remove(archivePath) }()

	if err := upgradeDownloadFunc(ctx, assetURL, archivePath); err != nil {
		return fmt.Errorf("downloading release: %w", err)
	}

	tmpBin, err := os.CreateTemp("", "marlin-upgrade-bin-*")
	if err != nil {
		return fmt.Errorf("creating temp binary file: %w", err)
	}
	binPath := tmpBin.Name()
	_ = tmpBin.Close()
	defer func() { _ = os.Remove(binPath) }()

	if err := upgradeExtractFunc(archivePath, "marlin", binPath); err != nil {
		return fmt.Errorf("extracting binary: %w", err)
	}

	_, _ = fmt.Fprintf(w, "installing to %s...\n", exe)
	ok, err = upgradeInstallFunc(w, binPath, exe)
	if err != nil {
		return fmt.Errorf("installing: %w", err)
	}
	if !ok {
		return nil
	}

	_, _ = fmt.Fprintf(w, "marlin upgraded to %s\n", latest)
	return nil
}
