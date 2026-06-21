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

type listItem struct {
	Slug    string `json:"slug"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	ModelID string `json:"model_id"`
	Active  bool   `json:"active"`
}

func runList(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	models, names, err := config.ListModelsFromDirs(effectiveDirs(cfg)...)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}

	if len(names) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no models found — run 'marlin add' to create one")
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	items := make([]listItem, len(names))
	for i, slug := range names {
		items[i] = listItem{
			Slug:    slug,
			Type:    string(models[i].Model.Type),
			Status:  string(models[i].Model.Status),
			ModelID: models[i].Model.ID,
			Active:  slug == cur.ActiveModel,
		}
	}

	w := cmd.OutOrStdout()

	switch outputFormat {
	case "json":
		return writeJSON(w, items)
	case "jsonl":
		for _, it := range items {
			if err := writeJSONLine(w, it); err != nil {
				return err
			}
		}
		return nil
	case "plain":
		for _, it := range items {
			active := ""
			if it.Active {
				active = "active"
			}
			_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.Slug, it.Type, it.Status, it.ModelID, active)
			if err != nil {
				return err
			}
		}
		return nil
	default: // table
		if _, err := fmt.Fprintf(w, "%-30s %-6s %-10s %s\n", "SLUG", "TYPE", "STATUS", "MODEL ID"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%-30s %-6s %-10s %s\n", "----", "----", "------", "--------"); err != nil {
			return err
		}
		for _, it := range items {
			active := ""
			if it.Active {
				active = " ◀ active"
			}
			if _, err := fmt.Fprintf(w, "%-30s %-6s %-10s %s%s\n",
				it.Slug, it.Type, it.Status, it.ModelID, active); err != nil {
				return err
			}
		}
		return nil
	}
}
