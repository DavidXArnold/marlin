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

func runPs(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	runner, err := buildAdhocRunner(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner: %w", err)
	}

	items, err := runner.List(cmd.Context())
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	if len(items) == 0 {
		_, err := fmt.Fprintln(w, "no marlin-managed containers running")
		return err
	}

	if _, err := fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n", "MODEL", "PROVIDER", "STATUS", "PORT", "CONTAINER ID"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n", "-----", "--------", "------", "----", "------------"); err != nil {
		return err
	}
	for _, c := range items {
		if _, err := fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n",
			c.Slug, c.Provider, c.Status, c.Port, shortID(c.ID)); err != nil {
			return err
		}
	}
	return nil
}
