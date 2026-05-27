package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/BurntSushi/toml"

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
}

func runAdd(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	result, err := runAddWizardFunc()
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), err)
		return nil
	}

	destDir := cfg.Paths.ModelsDir
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
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(result.Cfg); err != nil {
		return fmt.Errorf("writing model config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", destPath)
	return nil
}
