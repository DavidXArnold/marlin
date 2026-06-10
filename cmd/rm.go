package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
)

var rmCmd = &cobra.Command{
	Use:   "rm <model>",
	Short: "Remove a model profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runRm,
}

func init() {
	rootCmd.AddCommand(rmCmd)
}

func runRm(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	slug := args[0]
	path, err := config.FindModelPath(slug, effectiveDirs(cfg)...)
	if err != nil {
		return fmt.Errorf("model %q not found", slug)
	}

	if err := privilege.PromptAndRemove(cmd.OutOrStdout(), path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
	return err
}
