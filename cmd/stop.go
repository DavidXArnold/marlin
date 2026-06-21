package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/history"
	"github.com/DavidXArnold/marlin/internal/state"
)

var stopCmd = &cobra.Command{
	Use:   "stop [model]",
	Short: "Stop the active model or a marlin-managed ad-hoc container",
	Long: `Stop the active managed model (started with marlin start/switch) or an
ad-hoc container started with marlin run.

Without an argument, stops the active managed model and records the stop time in
state so marlin status and marlin start reflect it. If no managed model is active,
stops all ad-hoc containers instead.

With a model slug that matches the active model, stops that managed model.
With any other slug, stops the matching ad-hoc container.`,
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

	w := cmd.OutOrStdout()
	cur, _ := state.Load(cfg.Paths.StateFile)

	if len(args) == 0 {
		if cur.ActiveModel != "" {
			return stopActiveModel(cmd, cfg, cur, w)
		}
		// No active model — fall back to stopping all ad-hoc containers.
		runner, err := buildAdhocRunner(cfg)
		if err != nil {
			return fmt.Errorf("initialising runner: %w", err)
		}
		if err := runner.StopAll(cmd.Context()); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, "stopped all marlin-managed containers")
		return err
	}

	slug := args[0]
	if cur.ActiveModel == slug {
		return stopActiveModel(cmd, cfg, cur, w)
	}

	// Not the active model — treat as ad-hoc container.
	runner, err := buildAdhocRunner(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner: %w", err)
	}
	if err := runner.Stop(cmd.Context(), slug); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "stopped %s\n", slug)
	return err
}

// stopActiveModel stops the current managed provider, persists stop time in state,
// and prints a confirmation.
func stopActiveModel(cmd *cobra.Command, cfg *config.Config, cur *state.State, w io.Writer) error {
	p, err := buildProvider(cur.ActiveProvider, cfg)
	if err != nil {
		return fmt.Errorf("initialising provider: %w", err)
	}
	if err := p.Stop(cmd.Context()); err != nil {
		return err
	}
	state.RecordStop(cur)
	if saveErr := state.SavePrivileged(w, cfg.Paths.StateFile, cur); saveErr != nil {
		_, _ = fmt.Fprintf(w, "warning: could not save state: %v\n", saveErr)
	}

	var durationS float64
	if startedAt, ok := cur.ModelHistory[cur.ActiveModel]; ok && !startedAt.IsZero() {
		durationS = time.Since(startedAt).Seconds()
	}
	if herr := appendHistory(cfg.Paths.HistoryFile, history.HistoryEvent{
		Timestamp: time.Now(),
		Event:     "stop",
		Slug:      cur.ActiveModel,
		Provider:  string(cur.ActiveProvider),
		DurationS: durationS,
	}); herr != nil {
		_, _ = fmt.Fprintf(w, "warning: history: %v\n", herr)
	}

	_, err = fmt.Fprintf(w, "stopped %s\n", cur.ActiveModel)
	return err
}
