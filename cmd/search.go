package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
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
	searchCmd.Flags().Bool("global", false, "Install to system models dir when adding a result")
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

var pickSearchResult = ui.PickSearchResult
var searchActionMenu = ui.SearchActionMenu
var modelURL = ui.ModelURL

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
	global, _ := cmd.Flags().GetBool("global")
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

	// Warn about registries that will be skipped or have limited results due to missing credentials.
	for _, name := range regs {
		switch name {
		case "ngc":
			if sec["NGC_API_KEY"] == "" {
				if err := writeErrf("notice: NGC not searched — run 'marlin configure' to add NGC_API_KEY\n"); err != nil {
					return err
				}
				if err := writeErrf("        generate a key at https://org.ngc.nvidia.com/setup/personal-keys\n"); err != nil {
					return err
				}
			}
		case "huggingface":
			if sec["HF_TOKEN"] == "" {
				if err := writeErrf("notice: HF_TOKEN not configured — gated models will be excluded from results\n"); err != nil {
					return err
				}
				if err := writeErrf("        run 'marlin configure' to add your HuggingFace token\n"); err != nil {
					return err
				}
			}
		}
	}

	registries := buildRegistries(regs, sec)

	if Verbosity > 0 {
		type verboseSetter interface {
			SetVerbose(w io.Writer, level int)
		}
		for _, r := range registries {
			if vs, ok := r.(verboseSetter); ok {
				vs.SetVerbose(cmd.ErrOrStderr(), Verbosity)
			}
		}
	}

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
				m.DisplayName(),
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

	selected, err := pickSearchResult(allResults, freeVRAM)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil // user cancelled
	}

	url := modelURL(*selected)
	action, err := searchActionMenu(selected.ID, url)
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
		return addFromSearchResult(cfg, *selected, w, global)

	case ui.SearchActionRun:
		return runFromSearchResult(cmd, cfg, *selected, w)
	}

	return nil
}

// addFromSearchResult derives a slug and writes a model TOML to the models dir.
// If the dir is not user-writable, it warns and prompts before writing via sudo.
func addFromSearchResult(cfg *config.Config, m registry.ModelInfo, w io.Writer, global bool) error {
	slug := ui.AutoSlug(m.ID)
	destDir := installDir(cfg, global)
	path := filepath.Join(destDir, slug+".toml")

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("model %q already exists at %s", slug, path)
	}

	mc := modelConfigFromInfo(m, cfg.Server.Alias)

	data, err := config.ModelConfigToBytes(mc)
	if err != nil {
		return err
	}

	written, err := privilege.PromptAndWriteFile(w, destDir, path, data)
	if err != nil {
		return err
	}
	if !written {
		return nil // user cancelled
	}

	_, err = fmt.Fprintf(w, "created %s\n", path)
	return err
}

// runFromSearchResult builds an in-memory model config from the search result,
// writes it to a temp dir, and runs the model ad-hoc in the foreground.
func runFromSearchResult(cmd *cobra.Command, cfg *config.Config, m registry.ModelInfo, w io.Writer) error {
	slug := ui.AutoSlug(m.ID)
	mc := modelConfigFromInfo(m, cfg.Server.Alias)

	tmpDir, err := os.MkdirTemp("", "marlin-run-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := config.SaveModel(filepath.Join(tmpDir, slug+".toml"), mc); err != nil {
		return err
	}

	tmpCfg := *cfg
	tmpCfg.Paths.ModelsDir = tmpDir

	runner, err := buildAdhocRunner(&tmpCfg)
	if err != nil {
		return fmt.Errorf("initialising runner: %w", err)
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if _, err := fmt.Fprintf(w, "running %s (Ctrl-C to stop and remove)\n", slug); err != nil {
		return err
	}
	return runner.RunForeground(ctx, slug, w)
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
		return "-"
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
		return "-"
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
