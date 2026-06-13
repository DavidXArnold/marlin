package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/ui"
)

var logsCmd = &cobra.Command{
	Use:   "logs [model]",
	Short: "Show logs for the active or a named model",
	Long: `Show logs for the inference service or a running ad-hoc container.

With no argument marlin automatically selects the target:
  • Only one model running → its logs are shown immediately.
  • Multiple running → interactive picker lets you choose.
  • Nothing running → logs from the most recently stopped ad-hoc container
    are shown (useful for debugging startup failures).

With a model name, logs for that specific model are shown regardless of state.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	logsCmd.Flags().Int("lines", 100, "Number of lines to show")
}

// logsTarget describes which log source to use.
type logsTarget struct {
	label    string
	useAdhoc bool // false → managed provider Logs(); true → runner.LogsFor(slug)
	slug     string
}

// resolveLogsTargetFunc is injectable for tests.
var resolveLogsTargetFunc = resolveLogsTarget

func resolveLogsTarget(
	query string,
	cur *state.State,
	runner adhocRunner,
	cmd *cobra.Command,
) (logsTarget, error) {
	ctx := cmd.Context()

	// Collect all running adhoc containers.
	var running []provider.AdhocInfo
	if runner != nil {
		if all, err := runner.List(ctx); err == nil {
			for _, a := range all {
				if a.Status == "running" {
					running = append(running, a)
				}
			}
		}
	}

	// Explicit query: prefer an adhoc container matching the slug, else managed.
	if query != "" {
		for _, a := range running {
			if a.Slug == query {
				return logsTarget{label: query + " (adhoc)", useAdhoc: true, slug: query}, nil
			}
		}
		// Named model → assume managed service.
		return logsTarget{label: query, useAdhoc: false}, nil
	}

	// No query: build candidate list.
	var candidates []logsTarget
	for _, a := range running {
		desc := a.Status
		if a.Port != "" {
			desc += " :" + a.Port
		}
		candidates = append(candidates, logsTarget{
			label:    fmt.Sprintf("%s  [adhoc %s]", a.Slug, desc),
			useAdhoc: true,
			slug:     a.Slug,
		})
	}
	if cur.ActiveModel != "" {
		candidates = append(candidates, logsTarget{
			label:    fmt.Sprintf("%s  [managed %s]", cur.ActiveModel, cur.ActiveProvider),
			useAdhoc: false,
		})
	}

	switch len(candidates) {
	case 0:
		// Nothing running: fall back to most recently stopped adhoc container.
		if runner != nil {
			if all, _ := runner.List(ctx); len(all) > 0 {
				a := all[0]
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"no running models — showing last stopped container (%s %s)\n", a.Slug, a.Status)
				return logsTarget{label: a.Slug + " (stopped)", useAdhoc: true, slug: a.Slug}, nil
			}
		}
		return logsTarget{}, fmt.Errorf("no running models found — start one with 'marlin run' or 'marlin start'")
	case 1:
		return candidates[0], nil
	default:
		items := make([]ui.StringItem, len(candidates))
		for i, c := range candidates {
			items[i] = ui.StringItem{Label: c.label}
		}
		idx, err := ui.PickStrings(items, "choose model to view logs")
		if err != nil {
			return logsTarget{}, err
		}
		return candidates[idx], nil
	}
}

func runLogs(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)
	follow, _ := cmd.Flags().GetBool("follow")
	lines, _ := cmd.Flags().GetInt("lines")
	w := cmd.OutOrStdout()

	var runner adhocRunner
	if r, err := buildAdhocRunner(cfg); err == nil {
		runner = r
	}

	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	target, err := resolveLogsTargetFunc(query, cur, runner, cmd)
	if err != nil {
		return err
	}

	if target.useAdhoc {
		if runner == nil {
			return fmt.Errorf("docker is not available; cannot show ad-hoc container logs")
		}
		return runner.LogsFor(cmd.Context(), target.slug, w, follow, lines)
	}

	p, err := buildProvider(cur.ActiveProvider, cfg)
	if err != nil {
		return err
	}
	return p.Logs(cmd.Context(), w, follow, lines)
}
