package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/state"
)

var restartCmd = &cobra.Command{
	Use:   "restart [model]",
	Short: "Stop and restart the active model, or pick one",
	Long: `Stop the currently active model and start it again (or start a different one).

Without an argument: if a model is active, stops it and restarts it. If no model
is active, shows an interactive picker (same as marlin start).

With a model name: stops the active model (if any) and starts the named one.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
	restartCmd.Flags().Bool("enable", false, "Also enable the systemd unit to start at boot")
	restartCmd.Flags().BoolP("logs", "l", false, "Stream container/service logs while waiting for the API")
	restartCmd.Flags().String("max-runtime", "", "Stop the model after this duration (e.g. 15m, 1h); 0 = disabled")
}

func runRestart(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)
	w := cmd.OutOrStdout()

	// No arg and no active model → fall through to marlin start (interactive picker).
	if len(args) == 0 && cur.ActiveModel == "" {
		return runStart(cmd, args)
	}

	// No arg but there is an active model → stop it then restart it.
	if len(args) == 0 {
		target := cur.ActiveModel
		if err := stopActiveModel(cmd, cfg, cur, w); err != nil {
			return fmt.Errorf("stopping %s: %w", target, err)
		}
		return runStart(cmd, []string{target})
	}

	// Arg provided: stop the active model (if any, and different), then start the named one.
	target := args[0]
	if cur.ActiveModel != "" && cur.ActiveModel != target {
		if err := stopActiveModel(cmd, cfg, cur, w); err != nil {
			_, _ = fmt.Fprintf(w, "warning: could not stop %s: %v\n", cur.ActiveModel, err)
		}
	} else if cur.ActiveModel == target {
		if err := stopActiveModel(cmd, cfg, cur, w); err != nil {
			return fmt.Errorf("stopping %s: %w", target, err)
		}
	}
	return runStart(cmd, []string{target})
}
