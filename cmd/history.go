package cmd

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/history"
)

// appendHistory is injectable for tests.
var appendHistory = history.Append

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show model switch/stop event history",
	Args:  cobra.NoArgs,
	RunE:  runHistory,
}

var historyStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show history statistics",
	Args:  cobra.NoArgs,
	RunE:  runHistoryStats,
}

func init() {
	historyCmd.Flags().Int("last", 20, "number of most recent entries to show")
	historyCmd.Flags().String("slug", "", "filter by model slug")
	historyCmd.Flags().String("since", "", "filter entries since duration (e.g. 7d, 24h)")
	historyCmd.AddCommand(historyStatsCmd)
	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	last, _ := cmd.Flags().GetInt("last")
	slugFilter, _ := cmd.Flags().GetString("slug")
	sinceStr, _ := cmd.Flags().GetString("since")

	events, err := history.Load(cfg.Paths.HistoryFile, 0)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	var sinceTime time.Time
	if sinceStr != "" {
		d, parseErr := parseDurationArg(sinceStr)
		if parseErr != nil {
			return fmt.Errorf("invalid --since value %q: %w", sinceStr, parseErr)
		}
		sinceTime = time.Now().Add(-d)
	}

	var filtered []history.HistoryEvent
	for _, ev := range events {
		if slugFilter != "" && ev.Slug != slugFilter {
			continue
		}
		if !sinceTime.IsZero() && ev.Timestamp.Before(sinceTime) {
			continue
		}
		filtered = append(filtered, ev)
	}

	if last > 0 && len(filtered) > last {
		filtered = filtered[len(filtered)-last:]
	}

	w := cmd.OutOrStdout()

	if len(filtered) == 0 {
		if outputFormat == "json" {
			return writeJSON(w, []history.HistoryEvent{})
		}
		_, err = fmt.Fprintln(w, "no history")
		return err
	}

	switch outputFormat {
	case "json":
		return writeJSON(w, filtered)
	case "jsonl":
		for _, ev := range filtered {
			if err := writeJSONLine(w, ev); err != nil {
				return err
			}
		}
		return nil
	case "plain":
		for _, ev := range filtered {
			ts := ev.Timestamp.UTC().Format(time.RFC3339)
			_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%.3f\t%.3f\n",
				ts, ev.Event, ev.Slug, ev.ElapsedS, ev.DurationS)
			if err != nil {
				return err
			}
		}
		return nil
	default: // table
		_, _ = fmt.Fprintf(w, "%-21s %-15s %-22s %s\n",
			"TIME", "EVENT", "SLUG", "ELAPSED/DURATION")
		_, _ = fmt.Fprintln(w, strings.Repeat("-", 75))
		for _, ev := range filtered {
			ts := ev.Timestamp.Local().Format("2006-01-02 15:04:05")
			extra := ""
			if ev.ElapsedS > 0 {
				extra = fmt.Sprintf("%.1fs", ev.ElapsedS)
			} else if ev.DurationS > 0 {
				extra = fmt.Sprintf("%.0fs", ev.DurationS)
			}
			_, _ = fmt.Fprintf(w, "%-21s %-15s %-22s %s\n", ts, ev.Event, ev.Slug, extra)
		}
		return nil
	}
}

func runHistoryStats(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	events, err := history.Load(cfg.Paths.HistoryFile, 0)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	var (
		totalSwitches int
		slugsSeen     = make(map[string]bool)
		readyTimes    []float64
		sessionTimes  []float64
		crashes       int
	)

	for _, ev := range events {
		switch ev.Event {
		case "switch_start":
			totalSwitches++
			if ev.Slug != "" {
				slugsSeen[ev.Slug] = true
			}
		case "switch_ready":
			if ev.ElapsedS > 0 {
				readyTimes = append(readyTimes, ev.ElapsedS)
			}
		case "stop":
			if ev.DurationS > 0 {
				sessionTimes = append(sessionTimes, ev.DurationS)
			}
		case "crash":
			crashes++
		}
	}

	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "total switches : %d\n", totalSwitches)
	_, _ = fmt.Fprintf(w, "unique models  : %d\n", len(slugsSeen))

	if len(readyTimes) > 0 {
		_, _ = fmt.Fprintf(w, "avg ready time : %.1fs\n", average(readyTimes))
	} else {
		_, _ = fmt.Fprintf(w, "avg ready time : -\n")
	}

	if len(sessionTimes) > 0 {
		_, _ = fmt.Fprintf(w, "avg session    : %s\n", formatSeconds(average(sessionTimes)))
	} else {
		_, _ = fmt.Fprintf(w, "avg session    : -\n")
	}

	_, _ = fmt.Fprintf(w, "crashes        : %d\n", crashes)
	return nil
}

// parseDurationArg parses strings like "7d", "24h", "30m". Days are treated as 24h.
func parseDurationArg(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n := 0
		if _, err := fmt.Sscanf(s, "%dd", &n); err != nil {
			return 0, fmt.Errorf("cannot parse %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func formatSeconds(s float64) string {
	d := time.Duration(math.Round(s)) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
