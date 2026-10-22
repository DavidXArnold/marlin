package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/history"
	"github.com/DavidXArnold/marlin/internal/mesh"
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
	case config.ProviderLlamaCpp:
		unitPath := render.LlamaCppUnitPath(cfg)
		if _, err := os.Stat(unitPath); os.IsNotExist(err) {
			return fmt.Errorf("systemd unit %q is not installed — run 'marlin install --provider llamacpp' first", cfg.Service.LlamaCppUnit)
		}
	}
	return nil
}

// portSharingProviders are the systemd-managed providers that all bind
// server.port, so at most one of them can be serving at a time.
var portSharingProviders = []config.ProviderType{config.ProviderVLLM, config.ProviderLlamaCpp}

// stopConflictingServices stops any other port-sharing provider whose unit is
// live, judged by systemd rather than state.toml: state can be stale (a manual
// systemctl start, a failed state write), and a forgotten service would hold
// the port and crash-loop the new one.
func stopConflictingServices(ctx context.Context, w io.Writer, cfg *config.Config, target config.ProviderType) {
	for _, pt := range portSharingProviders {
		if pt == target {
			continue
		}
		p, err := buildProvider(pt, cfg)
		if err != nil {
			continue
		}
		st, err := p.Status(ctx)
		if err != nil || (!st.Running && st.State != "starting") {
			continue
		}
		if err := p.Stop(ctx); err != nil {
			_, _ = fmt.Fprintf(w, "warning: stopping %s service: %v\n", pt, err)
			continue
		}
		_, _ = fmt.Fprintf(w, "stopped running %s service\n", pt)
	}
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

	// Filter abstract models out of the picker — they are base configs only.
	concreteModels := models[:0:0]
	concreteNames := names[:0:0]
	for i, m := range models {
		if !m.Model.Abstract {
			concreteModels = append(concreteModels, models[i])
			concreteNames = append(concreteNames, names[i])
		}
	}

	// Load current state early so resolveModel can mark the active model.
	cur, _ := state.Load(cfg.Paths.StateFile)

	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	targetSlug, err := resolveModel(query, concreteNames, concreteModels, cur.ActiveModel, cur.ModelHistory)
	if err != nil {
		return err
	}

	targetModel, err := config.ResolveModel(targetSlug, dirs...)
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

	stopConflictingServices(cmd.Context(), cmd.OutOrStdout(), cfg, targetModel.Model.Type)

	// Execute the switch.
	p, err := buildProvider(targetModel.Model.Type, cfg)
	if err != nil {
		return err
	}

	switchStart := time.Now()
	if herr := appendHistory(cfg.Paths.HistoryFile, history.HistoryEvent{
		Timestamp: switchStart,
		Event:     "switch_start",
		Slug:      targetSlug,
		Provider:  string(targetModel.Model.Type),
		FromSlug:  cur.ActiveModel,
	}); herr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: history: %v\n", herr)
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
	// Capture image digest for NIM providers so marlin update can detect changes.
	if dg, ok := p.(nimDigester); ok && targetModel.Model.Image != "" {
		if digest, err := dg.GetDigest(cmd.Context(), targetModel.Model.Image); err == nil {
			newState.PinnedDigest = digest
		}
	}
	state.RecordStart(newState, targetSlug)
	if err := state.SavePrivileged(cmd.ErrOrStderr(), cfg.Paths.StateFile, newState); err != nil {
		if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save state: %v\n", err); writeErr != nil {
			return writeErr
		}
	}

	if herr := appendHistory(cfg.Paths.HistoryFile, history.HistoryEvent{
		Timestamp: time.Now(),
		Event:     "switch_ready",
		Slug:      targetSlug,
		Provider:  string(targetModel.Model.Type),
		ElapsedS:  time.Since(switchStart).Seconds(),
	}); herr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: history: %v\n", herr)
	}

	// Integration 1: auto-register the new endpoint with a running mesh-llm peer.
	meshAutoRegisterFunc(cmd, cfg, targetModel.Model.Type)

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "switched to %s (%s)\n", targetSlug, targetModel.Model.Type)
	return err
}

// meshAutoRegisterFunc is injectable for tests.
var meshAutoRegisterFunc = func(cmd *cobra.Command, cfg *config.Config, pt config.ProviderType) {
	if !cfg.Mesh.AutoRegister || pt == config.ProviderMesh {
		return
	}
	vLLMURL := fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
	changed, err := mesh.PatchOpenAIEndpoint(cfg.Mesh.ConfigPath, vLLMURL)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: mesh auto-register: %v\n", err)
		return
	}
	if changed {
		if err := mesh.NewClient(cfg.Mesh.ManagementURL).ApplyConfig(cmd.Context()); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: mesh apply-config: %v\n", err)
		}
	}
}
