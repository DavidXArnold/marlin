package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/ui"
	"github.com/DavidXArnold/marlin/internal/validate"
	"github.com/DavidXArnold/marlin/pkg/render"
)

var switchCmd = &cobra.Command{
	Use:   "switch [model]",
	Short: "Switch active model and restart the inference service",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

// preflightCheckFunc is injectable for tests.
var preflightCheckFunc = preflightForProvider

// preflightForProvider validates that the target provider is ready to run before
// we tear down the currently running one. Returns an error if the switch should
// be aborted.
func preflightForProvider(targetType config.ProviderType, cfg *config.Config) error {
	switch targetType {
	case config.ProviderVLLM:
		// Verify the systemd unit file exists so we don't stop NIM only to
		// fail on "unit not found" mid-switch.
		unitPath := render.SystemdUnitPath(cfg)
		if _, err := os.Stat(unitPath); os.IsNotExist(err) {
			return fmt.Errorf("systemd unit %q is not installed — run 'marlin install' first", cfg.Service.SystemdUnit)
		}
	}
	return nil
}

func runSwitch(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	dirs := effectiveDirs(cfg)
	models, names, err := config.ListModelsFromDirs(dirs...)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}

	// Load current state early so resolveModel can mark the active model.
	cur, _ := state.Load(cfg.Paths.StateFile)

	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	targetSlug, err := resolveModel(query, names, models, cur.ActiveModel, cur.ModelHistory)
	if err != nil {
		return err
	}

	modelPath, err := config.FindModelPath(targetSlug, dirs...)
	if err != nil {
		return err
	}

	targetModel, err := config.LoadModel(modelPath)
	if err != nil {
		return err
	}

	// Validation — errors block, warnings are printed.
	issues := validate.Model(targetModel, cfg.Server.Alias)
	for _, iss := range issues {
		if iss.Level == validate.LevelError {
			return fmt.Errorf("validation: %s", iss.Message)
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", iss.Message); err != nil {
			return err
		}
	}

	if cur.ActiveModel != "" &&
		cur.ActiveProvider != targetModel.Model.Type &&
		!cfg.Behavior.AllowTypeSwitch {
		return fmt.Errorf("switching from %s to %s is disabled (allow_type_switch = false in config)",
			cur.ActiveProvider, targetModel.Model.Type)
	}

	// Pre-flight: verify the target provider is ready before we tear down the
	// current one. For vLLM this means the systemd unit must already exist.
	if err := preflightCheckFunc(targetModel.Model.Type, cfg); err != nil {
		return err
	}

	// Confirmation prompt (before privilege escalation so it runs as the user).
	if cfg.Behavior.SwitchPrompt {
		ok, err := ui.Confirm(fmt.Sprintf("Switch to %q?", targetSlug))
		if err != nil || !ok {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "cancelled")
			return err
		}
	}

	// Warn if system load is high.
	checkSystemResources(cfg, cmd.ErrOrStderr())

	// Stop old provider if the type is changing.
	// Exit code 5 (unit not found) from systemctl stop is treated as a no-op in
	// SystemdManager.Stop — so vLLM → NIM switches where the unit was never
	// started don't print a spurious warning.
	if cur.ActiveModel != "" && cur.ActiveProvider != targetModel.Model.Type {
		oldProvider, err := buildProvider(cur.ActiveProvider, cfg)
		if err == nil {
			if stopErr := oldProvider.Stop(cmd.Context()); stopErr != nil {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: stopping previous provider: %v\n", err); err != nil {
					return err
				}
			} else if cur.ActiveProvider != targetModel.Model.Type {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "stopped %s (%s)\n", cur.ActiveModel, cur.ActiveProvider)
			}
		}
	}

	// Execute the switch.
	p, err := buildProvider(targetModel.Model.Type, cfg)
	if err != nil {
		return err
	}

	if err := p.Switch(cmd.Context(), targetSlug); err != nil {
		return err
	}

	// Persist state, carrying over history and recording this start.
	newState := &state.State{
		ActiveModel:    targetSlug,
		ActiveProvider: targetModel.Model.Type,
		ModelHistory:   cur.ModelHistory,
	}
	if st, err := p.Status(cmd.Context()); err == nil {
		newState.ContainerID = st.ContainerID
	}
	state.RecordStart(newState, targetSlug)
	if err := state.SavePrivileged(cmd.ErrOrStderr(), cfg.Paths.StateFile, newState); err != nil {
		if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save state: %v\n", err); writeErr != nil {
			return writeErr
		}
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "switched to %s (%s)\n", targetSlug, targetModel.Model.Type)
	return err
}
