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
		fmt.Fprintln(w, "no marlin-managed containers running")
		return nil
	}

	fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n", "MODEL", "PROVIDER", "STATUS", "PORT", "CONTAINER ID")
	fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n", "-----", "--------", "------", "----", "------------")
	for _, c := range items {
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Fprintf(w, "%-20s %-8s %-10s %-6s %s\n",
			c.Slug, c.Provider, c.Status, c.Port, id)
	}
	return nil
}
