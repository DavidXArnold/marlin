package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/ui"
)

// rmConfirmFunc is injectable for tests.
var rmConfirmFunc = ui.Confirm

var rmCmd = &cobra.Command{
	Use:   "rm [model]",
	Short: "Remove a model profile",
	Args:  cobra.MaximumNArgs(1),
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

	dirs := effectiveDirs(cfg)
	models, names, err := config.ListModelsFromDirs(dirs...)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	slug, err := resolveModel(query, names, models, cur.ActiveModel, cur.ModelHistory)
	if err != nil {
		return err
	}

	path, err := config.FindModelPath(slug, dirs...)
	if err != nil {
		return fmt.Errorf("model %q not found", slug)
	}

	ok, err := rmConfirmFunc(fmt.Sprintf("Remove %q (%s)?", slug, path))
	if err != nil {
		return err
	}
	if !ok {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "cancelled")
		return err
	}

	if err := privilege.PromptAndRemove(cmd.OutOrStdout(), path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
	return err
}
