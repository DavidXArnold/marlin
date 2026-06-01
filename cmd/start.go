package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/internal/state"
)

var startCmd = &cobra.Command{
	Use:   "start [model]",
	Short: "Start the inference service, optionally selecting a model",
	Long: `Start the inference service.

Without arguments: if a model is already active and the service is stopped,
it is restarted without switching models. If no model has been activated yet,
an interactive picker lets you choose one.

With a model name: behaves like 'marlin switch <model>'.

Use --enable to also configure the systemd unit to start automatically at boot.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

// enableUnit is injectable for tests.
var enableUnit = func(cfg *config.Config) error {
	svc := service.NewSystemdManager(cfg.Service.SystemdUnit)
	if err := svc.Enable(rootCmd.Context()); err != nil {
		return fmt.Errorf("enabling %s at boot: %w", cfg.Service.SystemdUnit, err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().Bool("enable", false, "Also enable the systemd unit to start at boot")
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	enable, _ := cmd.Flags().GetBool("enable")
	cur, _ := state.Load(cfg.Paths.StateFile)

	// If no model was requested and we have an active model, try resuming it
	// without a full switch (avoids unnecessary service restarts).
	if len(args) == 0 && cur.ActiveModel != "" {
		p, err := buildProvider(cur.ActiveProvider, cfg)
		if err != nil {
			return fmt.Errorf("initialising provider: %w", err)
		}

		st, err := p.Status(cmd.Context())
		if err == nil && st.Running {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(),
				"already running %s (%s)\n", cur.ActiveModel, cur.ActiveProvider); err != nil {
				return err
			}
			if enable {
				return enableUnit(cfg)
			}
			return nil
		}

		// Service stopped — restart with the existing active model.
		if _, err := fmt.Fprintf(cmd.OutOrStdout(),
			"starting %s (%s)\n", cur.ActiveModel, cur.ActiveProvider); err != nil {
			return err
		}
		args = []string{cur.ActiveModel}
	}

	// Delegate to switch logic (handles picker, validation, privilege escalation).
	if err := runSwitch(cmd, args); err != nil {
		return err
	}

	if enable {
		return enableUnit(cfg)
	}
	return nil
}
