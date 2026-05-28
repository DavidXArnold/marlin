package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/registry"
	"github.com/DavidXArnold/marlin/internal/secrets"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
	"github.com/DavidXArnold/marlin/internal/ui"
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
	searchCmd.Flags().Bool("plain", false, "Plain table output; skip interactive picker")
}

// stdoutIsTerminal is injectable for tests.
var stdoutIsTerminal = func() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// openBrowserCmd is injectable for tests.
var openBrowserCmd = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

var newHuggingFaceRegistry = func(token string) registry.Registry {
	return registry.NewHuggingFace(token)
}

var newNGCRegistry = func(apiKey string) registry.Registry {
	return registry.NewNGC(apiKey)
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

	si, _ := sysinfo.Detect()
	freeVRAM := si.FreeVRAMMB()

	regs, _ := cmd.Flags().GetStringSlice("registry")
	plain, _ := cmd.Flags().GetBool("plain")
	query := args[0]
	w := cmd.OutOrStdout()
	writef := func(format string, args ...any) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	writeln := func(args ...any) error {
		_, err := fmt.Fprintln(w, args...)
		return err
	}
	writeErrf := func(format string, args ...any) error {
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), format, args...)
		return err
	}

	// Warn about registries that will be skipped due to missing credentials.
	for _, name := range regs {
		if name == "ngc" && sec["NGC_API_KEY"] == "" {
			if err := writeErrf("notice: NGC not searched — API key not configured\n"); err != nil {
				return err
			}
			if err := writeErrf("        run 'marlin configure' or generate a key at https://org.ngc.nvidia.com/setup/personal-keys\n"); err != nil {
				return err
			}
		}
	}

	registries := buildRegistries(regs, sec)

	// Collect all results across registries so the picker has the full set.
	var allResults []registry.ModelInfo
	type registryResults struct {
		name    string
		results []registry.ModelInfo
	}
	var perRegistry []registryResults

	for _, r := range registries {
		results, err := r.Search(cmd.Context(), query)
		if err != nil {
			if err := writeErrf("warning: %s search failed: %v\n", r.Name(), err); err != nil {
				return err
			}
			continue
		}
		perRegistry = append(perRegistry, registryResults{name: r.Name(), results: results})
		allResults = append(allResults, results...)
	}

	// Always print the table.
	for _, pr := range perRegistry {
		if len(pr.results) == 0 {
			if err := writef("[%s] no results\n", pr.name); err != nil {
				return err
			}
			continue
		}
		if err := writef("\n[%s]\n", pr.name); err != nil {
			return err
		}
		if err := writef("%-52s %-12s %-9s %-4s  %s\n",
			"ID", "UPDATED", "VRAM EST", "FIT", "DESCRIPTION"); err != nil {
			return err
		}
		if err := writef("%-52s %-12s %-9s %-4s  %s\n",
			"--", "-------", "--------", "---", "-----------"); err != nil {
			return err
		}

		for _, m := range pr.results {
			desc := m.Description
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			if err := writef("%-52s %-12s %-9s %-4s  %s\n",
				m.ID,
				formatUpdated(m.LastUpdated),
				formatVRAM(m.EstimatedVRAMMB()),
				fitLabel(m.EstimatedVRAMMB(), freeVRAM),
				desc,
			); err != nil {
				return err
			}
		}
	}

	// TUI picker: only when stdout is a terminal and --plain is not set.
	if plain || !stdoutIsTerminal() || len(allResults) == 0 {
		return nil
	}

	selected, err := ui.PickSearchResult(allResults, freeVRAM)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil // user cancelled
	}

	url := ui.ModelURL(*selected)
	action, err := ui.SearchActionMenu(selected.ID, url)
	if err != nil {
		return err
	}

	switch action {
	case ui.SearchActionBrowse:
		if url == "" {
			return writeln("no URL available for this model")
		}
		if err := writef("opening %s\n", url); err != nil {
			return err
		}
		if err := openBrowserCmd(url); err != nil {
			return fmt.Errorf("opening browser: %w", err)
		}

	case ui.SearchActionAdd:
		return addFromSearchResult(cfg, *selected, w)
	}

	return nil
}

// addFromSearchResult derives a slug and writes a model TOML to the models dir.
func addFromSearchResult(cfg *config.Config, m registry.ModelInfo, w io.Writer) error {
	slug := ui.AutoSlug(m.ID)
	path := filepath.Join(cfg.Paths.ModelsDir, slug+".toml")

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("model %q already exists at %s", slug, path)
	}

	mc := modelConfigFromInfo(m, cfg.Server.Alias)

	if err := config.SaveModel(path, mc); err != nil {
		if os.IsPermission(err) {
			requireRoot() // re-exec as sudo; search TUI repeats under root
		}
		return err
	}

	_, err := fmt.Fprintf(w, "created %s\n", path)
	return err
}

func modelConfigFromInfo(m registry.ModelInfo, serverAlias string) *config.ModelConfig {
	mc := &config.ModelConfig{
		Model: config.ModelMeta{
			Registry: m.Registry,
			Status:   config.StatusUntested,
		},
	}

	if m.Registry == "ngc" {
		mc.Model.Type = config.ProviderNIM
		mc.Model.Image = m.ID
	} else {
		mc.Model.Type = config.ProviderVLLM
		mc.Model.ID = m.ID
		var servedNames []string
		if serverAlias != "" {
			servedNames = []string{serverAlias}
		}
		mc.Serve = config.ServeConfig{
			Quantization:         m.Quantization,
			GPUMemoryUtilization: 0.90,
			ServedModelName:      servedNames,
		}
	}

	return mc
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
			out = append(out, newHuggingFaceRegistry(sec["HF_TOKEN"]))
		case "ngc":
			if sec["NGC_API_KEY"] == "" {
				continue // no key configured — skip silently
			}
			out = append(out, newNGCRegistry(sec["NGC_API_KEY"]))
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
