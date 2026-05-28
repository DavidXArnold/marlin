package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/registry"
	"github.com/DavidXArnold/marlin/internal/secrets"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search model registries (HuggingFace, NGC)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().StringSlice("registry", []string{"huggingface", "ngc"}, "Registries to search")
}

func runSearch(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	sec, err := secrets.Load(cfg.Paths.SecretsEnv)
	if err != nil {
		return fmt.Errorf("loading secrets: %w", err)
	}

	// Detect hardware for fit scoring — soft failure, freeVRAM stays 0.
	si, _ := sysinfo.Detect()
	freeVRAM := si.FreeVRAMMB()

	regs, _ := cmd.Flags().GetStringSlice("registry")
	query := args[0]
	w := cmd.OutOrStdout()

	registries := buildRegistries(regs, sec)

	for _, r := range registries {
		results, err := r.Search(cmd.Context(), query)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s search failed: %v\n", r.Name(), err)
			continue
		}
		if len(results) == 0 {
			fmt.Fprintf(w, "[%s] no results\n", r.Name())
			continue
		}

		fmt.Fprintf(w, "\n[%s]\n", r.Name())
		fmt.Fprintf(w, "%-52s %-12s %-9s %-4s  %s\n",
			"ID", "UPDATED", "VRAM EST", "FIT", "DESCRIPTION")
		fmt.Fprintf(w, "%-52s %-12s %-9s %-4s  %s\n",
			"--", "-------", "--------", "---", "-----------")

		for _, m := range results {
			desc := m.Description
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			fmt.Fprintf(w, "%-52s %-12s %-9s %-4s  %s\n",
				m.ID,
				formatUpdated(m.LastUpdated),
				formatVRAM(m.EstimatedVRAMMB()),
				fitLabel(m.EstimatedVRAMMB(), freeVRAM),
				desc,
			)
		}
	}

	return nil
}

func buildRegistries(names []string, sec map[string]string) []registry.Registry {
	seen := map[string]bool{}
	var out []registry.Registry
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		switch name {
		case "huggingface", "hf":
			out = append(out, registry.NewHuggingFace(sec["HF_TOKEN"]))
		case "ngc":
			out = append(out, registry.NewNGC(sec["NGC_API_KEY"]))
		}
	}
	return out
}

func formatUpdated(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	days := int(time.Since(t).Hours() / 24)
	switch {
	case days == 0:
		return "today"
	case days < 7:
		return fmt.Sprintf("%dd ago", days)
	case days < 30:
		return fmt.Sprintf("%dw ago", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo ago", days/30)
	default:
		return fmt.Sprintf("%dy ago", days/365)
	}
}

func formatVRAM(mb uint64) string {
	if mb == 0 {
		return "unknown"
	}
	return sysinfo.FormatMB(mb)
}

// fitLabel returns ✓, ~, ✗, or ? based on whether the model fits in free VRAM.
// ✓ = estimated ≤ 80% of free VRAM (comfortable fit)
// ~ = 80–100% of free VRAM (tight)
// ✗ = exceeds free VRAM
// ? = VRAM unknown on either side
func fitLabel(estimatedMB, freeVRAMMB uint64) string {
	if estimatedMB == 0 || freeVRAMMB == 0 {
		return "?"
	}
	ratio := float64(estimatedMB) / float64(freeVRAMMB)
	switch {
	case ratio <= 0.80:
		return "✓"
	case ratio <= 1.0:
		return "~"
	default:
		return "✗"
	}
}
