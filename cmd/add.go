package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/ui"
)

var runAddWizardFunc = ui.RunAddWizard

var addCmd = &cobra.Command{
	Use:   "add [registry-id]",
	Short: "Create a new model config interactively",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAdd,
}

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().Bool("global", false, "Install to system models dir ("+"/etc/marlin/models"+")")
}

func runAdd(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	result, err := runAddWizardFunc()
	if err != nil {
		if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), err); writeErr != nil {
			return writeErr
		}
		return nil
	}

	global, _ := cmd.Flags().GetBool("global")
	destDir := installDir(cfg, global)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating models dir: %w", err)
	}

	destPath := filepath.Join(destDir, result.Slug+".toml")
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("model %q already exists at %s", result.Slug, destPath)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating model file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := toml.NewEncoder(f).Encode(result.Cfg); err != nil {
		return fmt.Errorf("writing model config: %w", err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", destPath)
	return err
}
