package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List marlin-managed ad-hoc containers",
	RunE:  runPs,
}

func init() {
	rootCmd.AddCommand(psCmd)
}

type psItem struct {
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	Port        string `json:"port"`
	ContainerID string `json:"container_id"`
}

func runPs(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	runner, err := buildAdhocRunner(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner: %w", err)
	}

	containers, err := runner.List(cmd.Context())
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	if len(containers) == 0 {
		_, err := fmt.Fprintln(w, "no marlin-managed containers running")
		return err
	}

	items := make([]psItem, len(containers))
	for i, c := range containers {
		items[i] = psItem{
			Model:       c.Slug,
			Provider:    c.Provider,
			Status:      c.Status,
			Port:        c.Port,
			ContainerID: shortID(c.ID),
		}
	}

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
			_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.Model, it.Provider, it.Status, it.Port, it.ContainerID)
			if err != nil {
				return err
			}
		}
		return nil
	default: // table
		if _, err := fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n", "MODEL", "PROVIDER", "STATUS", "PORT", "CONTAINER ID"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n", "-----", "--------", "------", "----", "------------"); err != nil {
			return err
		}
		for _, it := range items {
			if _, err := fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n",
				it.Model, it.Provider, it.Status, it.Port, it.ContainerID); err != nil {
				return err
			}
		}
		return nil
	}
}
