package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
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
	destPath := filepath.Join(destDir, result.Slug+".toml")

	if _, statErr := config.FindModelPath(result.Slug, effectiveDirs(cfg)...); statErr == nil {
		return fmt.Errorf("model %q already exists", result.Slug)
	}

	w := cmd.OutOrStdout()
	maybeOfferUMAHint(result.Cfg, w)

	data, err := config.ModelConfigToBytes(result.Cfg)
	if err != nil {
		return fmt.Errorf("encoding model config: %w", err)
	}
	written, err := privilege.PromptAndWriteFile(w, destDir, destPath, data)
	if err != nil {
		return fmt.Errorf("writing model config: %w", err)
	}
	if !written {
		return nil // cancelled
	}

	_, err = fmt.Fprintf(w, "created %s\n", destPath)
	return err
}
