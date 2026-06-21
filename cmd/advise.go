package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/advise"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

var adviseCmd = &cobra.Command{
	Use:   "advise <model-id>",
	Short: "Recommend quantizations that fit your available VRAM",
	Long: `Estimates VRAM requirements for each quantization of the given model
and shows which ones fit your available GPU memory.

Example:
  marlin advise meta-llama/Llama-3.1-70B-Instruct`,
	Args: cobra.ExactArgs(1),
	RunE: runAdvise,
}

func init() {
	rootCmd.AddCommand(adviseCmd)
	adviseCmd.Flags().Bool("no-detect", false, "skip VRAM detection (show estimates only)")
}

// adviseDetectFunc is injectable for tests.
var adviseDetectFunc = func() (*sysinfo.SystemInfo, error) {
	return sysinfo.Detect()
}

func runAdvise(cmd *cobra.Command, args []string) error {
	modelID := args[0]
	w := cmd.OutOrStdout()

	noDetect, _ := cmd.Flags().GetBool("no-detect")

	var availVRAMMB uint64
	if !noDetect {
		info, err := adviseDetectFunc()
		if err == nil {
			availVRAMMB = info.TotalVRAMMB()
		}
	}

	recs := advise.Advise(modelID, availVRAMMB)

	params := advise.ParseParamsBillion(modelID)
	_, _ = fmt.Fprintf(w, "quant advisor: %s", modelID)
	if params > 0 {
		_, _ = fmt.Fprintf(w, "  (%.0fB params)", params)
	}
	_, _ = fmt.Fprintln(w)

	if availVRAMMB > 0 {
		_, _ = fmt.Fprintf(w, "available VRAM: %s\n", advise.FormatVRAMMB(availVRAMMB))
	} else {
		_, _ = fmt.Fprintln(w, "available VRAM: unknown (no GPU detected or --no-detect)")
	}
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintf(w, "%-14s  %-10s  %-6s\n", "QUANTIZATION", "EST. VRAM", "FITS?")
	_, _ = fmt.Fprintf(w, "%-14s  %-10s  %-6s\n", "------------", "---------", "-----")
	for _, r := range recs {
		fits := "—"
		if availVRAMMB > 0 {
			if r.Fits {
				fits = "✓"
			} else {
				fits = "✗"
			}
		}
		_, _ = fmt.Fprintf(w, "%-14s  %-10s  %s\n",
			r.Quant.Name,
			advise.FormatVRAMMB(r.EstVRAMMB),
			fits,
		)
	}

	best := advise.BestFit(recs)
	if best != nil {
		_, _ = fmt.Fprintf(w, "\nrecommendation: %s — search HF: %q\n",
			best.Quant.Label, best.SearchQuery)
	} else if availVRAMMB > 0 {
		_, _ = fmt.Fprintln(w, "\nno quantization fits available VRAM")
	}

	return nil
}
