package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Show (or remove) cached model files and stale container data",
	Long: `prune scans the HuggingFace model cache and NIM cache and reports
reclaimable disk space. By default it performs a dry run — pass --apply to
actually delete the files.

Use --hf-cache to control which HF cache directory is scanned (default:
~/.cache/huggingface/hub).`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().Bool("apply", false, "actually delete the identified files (default: dry run)")
	pruneCmd.Flags().String("hf-cache", "", "HuggingFace cache directory (default: ~/.cache/huggingface/hub)")
	rootCmd.AddCommand(pruneCmd)
}

// pruneEntry represents a single reclaimable item.
type pruneEntry struct {
	label  string
	path   string
	sizeGB float64
}

// pruneWalkFunc is injectable for tests.
var pruneWalkFunc = filepath.WalkDir

// pruneRemoveAll is injectable for tests.
var pruneRemoveAll = os.RemoveAll

func runPrune(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	apply, _ := cmd.Flags().GetBool("apply")
	hfCache, _ := cmd.Flags().GetString("hf-cache")
	if hfCache == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			hfCache = filepath.Join(home, ".cache", "huggingface", "hub")
		}
	}

	w := cmd.OutOrStdout()

	var entries []pruneEntry

	// Scan HF cache.
	if hfCache != "" {
		hfEntries, err := scanHFCache(hfCache)
		if err == nil {
			entries = append(entries, hfEntries...)
		}
	}

	// Scan NIM cache.
	if cfg.Paths.NIMCache != "" {
		nimEntries, err := scanNIMCache(cfg.Paths.NIMCache)
		if err == nil {
			entries = append(entries, nimEntries...)
		}
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, "nothing to prune")
		return nil
	}

	mode := "DRY RUN"
	if apply {
		mode = "APPLY"
	}
	_, _ = fmt.Fprintf(w, "prune [%s]\n\n", mode)

	var totalGB float64
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "  %-12s  %5.1f GiB  %s\n", e.label, e.sizeGB, e.path)
		totalGB += e.sizeGB
	}
	_, _ = fmt.Fprintf(w, "\ntotal reclaimable: %.1f GiB\n", totalGB)

	if !apply {
		_, _ = fmt.Fprintln(w, "\nrun with --apply to delete")
		return nil
	}

	var errs []string
	for _, e := range entries {
		if err := pruneRemoveAll(e.path); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.path, err))
		} else {
			_, _ = fmt.Fprintf(w, "deleted %s\n", e.path)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("prune errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

func scanHFCache(dir string) ([]pruneEntry, error) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []pruneEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		size := dirSizeGB(fullPath)
		// HF cache dirs are named like "models--org--model".
		label := "hf-cache"
		out = append(out, pruneEntry{label: label, path: fullPath, sizeGB: size})
	}
	return out, nil
}

func scanNIMCache(dir string) ([]pruneEntry, error) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []pruneEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		size := dirSizeGB(fullPath)
		out = append(out, pruneEntry{label: "nim-cache", path: fullPath, sizeGB: size})
	}
	return out, nil
}

func dirSizeGB(path string) float64 {
	var total int64
	_ = pruneWalkFunc(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return float64(total) / (1 << 30)
}

