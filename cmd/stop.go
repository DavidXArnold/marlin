package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop [model]",
	Short: "Stop and remove a marlin-managed ad-hoc container",
	Long: `Stop and remove one or all ad-hoc containers started with marlin run.

Without an argument, stops all marlin-managed containers.
With a model slug, stops only that container.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	runner, err := buildAdhocRunner(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner: %w", err)
	}

	w := cmd.OutOrStdout()

	if len(args) == 1 {
		slug := args[0]
		if err := runner.Stop(cmd.Context(), slug); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, "stopped %s\n", slug)
		return err
	}

	if err := runner.StopAll(cmd.Context()); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "stopped all marlin-managed containers")
	return err
}
