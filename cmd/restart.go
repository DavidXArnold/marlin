package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
)

var restartCmd = &cobra.Command{
	Use:   "restart [model]",
	Short: "Stop the active model and start it again (or pick a new one)",
	Long: `Stop the currently running model and start it again.

Without an argument, shows the current model status then an interactive picker
so you can restart the same model or choose a different one.

With a model name, stops the current model (if any) and starts the named one.`,
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

	var target string

	if len(args) > 0 {
		target = args[0]
	} else {
		// Interactive mode: show current status then a model picker.
		if cur.ActiveModel != "" {
			provStatus := "unknown"
			if p, buildErr := buildProvider(cur.ActiveProvider, cfg); buildErr == nil {
				if st, stErr := p.Status(cmd.Context()); stErr == nil {
					if st.Running {
						provStatus = "running"
					} else {
						provStatus = "stopped"
					}
				}
			}
			_, _ = fmt.Fprintf(w, "active: %s (%s, %s)\n", cur.ActiveModel, cur.ActiveProvider, provStatus)
		}

		dirs := effectiveDirs(cfg)
		models, names, listErr := config.ListModelsFromDirs(dirs...)
		if listErr != nil {
			return fmt.Errorf("listing models: %w", listErr)
		}

		target, err = resolveModel("", names, models, cur.ActiveModel, cur.ModelHistory)
		if err != nil {
			return err
		}
	}

	// Stop the current active model if one is running.
	if cur.ActiveModel != "" {
		_, _ = fmt.Fprintf(w, "stopping %s...\n", cur.ActiveModel)
		if stopErr := stopActiveModel(cmd, cfg, cur, w); stopErr != nil {
			if cur.ActiveModel == target {
				return fmt.Errorf("stopping %s: %w", cur.ActiveModel, stopErr)
			}
			_, _ = fmt.Fprintf(w, "warning: could not stop %s: %v\n", cur.ActiveModel, stopErr)
		}
	}

	return runStart(cmd, []string{target})
}
