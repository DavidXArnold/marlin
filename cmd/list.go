package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available model configurations",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	models, names, err := config.ListModels(cfg.Paths.ModelsDir)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}

	if len(names) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no models found — run 'marlin add' to create one")
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(w, "%-30s %-6s %-10s %s\n", "SLUG", "TYPE", "STATUS", "MODEL ID"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-30s %-6s %-10s %s\n", "----", "----", "------", "--------"); err != nil {
		return err
	}

	for i, slug := range names {
		m := models[i]
		active := ""
		if slug == cur.ActiveModel {
			active = " ◀ active"
		}
		if _, err := fmt.Fprintf(w, "%-30s %-6s %-10s %s%s\n",
			slug, m.Model.Type, m.Model.Status, m.Model.ID, active); err != nil {
			return err
		}
	}

	return nil
}
