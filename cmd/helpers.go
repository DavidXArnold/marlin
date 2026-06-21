package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/secrets"
	"github.com/DavidXArnold/marlin/internal/smoke"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
	"github.com/DavidXArnold/marlin/internal/ui"
)

// shortID truncates a container ID to 12 hex characters (Docker short-form).
func shortID(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// lineWriter wraps an io.Writer with error-returning printf/println helpers,
// eliminating repetitive fmt.Fprintf boilerplate in command handlers.
type lineWriter struct{ w io.Writer }

func (lw lineWriter) printf(format string, args ...any) error {
	_, err := fmt.Fprintf(lw.w, format, args...)
	return err
}

func (lw lineWriter) println(args ...any) error {
	_, err := fmt.Fprintln(lw.w, args...)
	return err
}

// confirmPrompt prints prompt to w, reads from r, and returns true if the
// response is "y" or "Y" (leading/trailing whitespace is ignored).
func confirmPrompt(w io.Writer, r io.Reader, prompt string) bool {
	_, _ = fmt.Fprint(w, prompt)
	buf := make([]byte, 4)
	n, _ := r.Read(buf)
	return strings.ToLower(strings.TrimSpace(string(buf[:n]))) == "y"
}

// globalConfig loads the typed config, falling back to defaults when no file exists.
func globalConfig() (*config.Config, error) {
	path := cfgFile
	if path == "" {
		candidates := []string{
			filepath.Join(os.Getenv("HOME"), ".config", "marlin", "config.toml"),
			"/etc/marlin/config.toml",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}
	return config.Load(path)
}

// buildProvider constructs the right Provider implementation for the given type.
// Declared as a var so tests can inject a mock without touching real systemd/Docker.
var buildProvider = func(pt config.ProviderType, cfg *config.Config) (provider.Provider, error) {
	switch pt {
	case config.ProviderVLLM, "":
		return provider.NewVLLMProvider(cfg, effectiveDirs(cfg)), nil
	case config.ProviderNIM:
		sec, err := secrets.Load(cfg.Paths.SecretsEnv)
		if err != nil {
			return nil, fmt.Errorf("loading secrets: %w", err)
		}
		if cfg.Service.ContainerRuntime == "containerd" {
			return provider.NewContainerdNIMProvider(cfg, sec["NGC_API_KEY"])
		}
		return provider.NewNIMProvider(cfg, sec["NGC_API_KEY"])
	case config.ProviderLlamaCpp:
		return provider.NewLlamaCppProvider(cfg, effectiveDirs(cfg)), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", pt)
	}
}

// effectiveDirs returns the ordered list of model directories to search.
// The user dir (ModelsDir) comes first so it takes precedence over GlobalModelsDir.
func effectiveDirs(cfg *config.Config) []string {
	dirs := []string{cfg.Paths.ModelsDir}
	if cfg.Paths.GlobalModelsDir != "" && cfg.Paths.GlobalModelsDir != cfg.Paths.ModelsDir {
		dirs = append(dirs, cfg.Paths.GlobalModelsDir)
	}
	return dirs
}

// installDir returns the directory to write a new model config to.
// When global is true or cfg.Behavior.GlobalInstall is set, uses GlobalModelsDir.
func installDir(cfg *config.Config, global bool) string {
	if global || cfg.Behavior.GlobalInstall {
		return cfg.Paths.GlobalModelsDir
	}
	return cfg.Paths.ModelsDir
}

// resolveModel resolves a user-supplied name (exact, fuzzy, or empty) to a
// model slug. Launches the interactive picker when the match is ambiguous or
// the query is empty. activeSlug is marked with ◀ in the picker (pass "" to skip).
// When query is non-empty, an ambiguous fuzzy match is an error — the picker is
// never shown if the user provided an explicit model identifier.
func resolveModel(query string, names []string, cfgs []*config.ModelConfig, activeSlug string, history map[string]time.Time) (string, error) {
	if len(names) == 0 {
		return "", fmt.Errorf("no models found in models directory — run 'marlin add' first")
	}

	if query != "" {
		matches := ui.FuzzyMatch(query, names)
		if len(matches) == 0 {
			return "", fmt.Errorf("no model matching %q", query)
		}
		if len(matches) == 1 || matches[0] == query {
			return matches[0], nil
		}
		return "", fmt.Errorf("ambiguous model %q — be more specific (matches: %v)", query, matches)
	}

	return ui.PickModel(names, cfgs, "", activeSlug, history)
}

// umaHintConfirmFunc is injectable for tests.
var umaHintConfirmFunc = ui.Confirm

// maybeOfferUMAHint checks if any detected GPU uses a unified memory architecture
// and the model is a NIM profile without NIM_PASSTHROUGH_ARGS already set. If so,
// it prompts the user to add the env var to the model config before it is written.
func maybeOfferUMAHint(mc *config.ModelConfig, w io.Writer) {
	if mc.Model.Type != config.ProviderNIM {
		return
	}
	for _, env := range mc.Serve.ExtraEnv {
		if strings.Contains(env, "NIM_PASSTHROUGH_ARGS") {
			return
		}
	}
	si, err := sysinfo.Detect()
	if err != nil {
		return
	}
	var hasUMA bool
	for _, g := range si.GPUs {
		if g.IsUMA {
			hasUMA = true
			break
		}
	}
	if !hasUMA {
		return
	}
	_, _ = fmt.Fprintln(w, "hint: UMA/unified-memory GPU detected — NIM may need a higher gpu_memory_utilization")
	ok, err := umaHintConfirmFunc(`Add extra_env = ["NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9"] to this profile?`)
	if err != nil || !ok {
		return
	}
	mc.Serve.ExtraEnv = append(mc.Serve.ExtraEnv, "NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9")
}

// effectiveMaxRuntime returns the active max-runtime duration: the --max-runtime
// flag takes precedence over behavior.max_runtime in config. Returns 0 if disabled.
func effectiveMaxRuntime(cmd *cobra.Command, cfg *config.Config) time.Duration {
	if f := cmd.Flags().Lookup("max-runtime"); f != nil && f.Changed {
		s, _ := cmd.Flags().GetString("max-runtime")
		d, _ := time.ParseDuration(s)
		return d
	}
	return cfg.Behavior.MaxRuntimeDuration()
}

// humanDuration returns a short human-readable string for d, e.g. "5 minutes", "2 hours".
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// smokeConfig converts BehaviorConfig smoke fields to a smoke.Config.
func smokeConfig(cfg *config.Config) smoke.Config {
	timeout := 30 * time.Second
	if cfg.Behavior.SmokeTestTimeout != "" {
		if d, err := time.ParseDuration(cfg.Behavior.SmokeTestTimeout); err == nil {
			timeout = d
		}
	}
	return smoke.Config{
		Enabled: cfg.Behavior.SmokeTest,
		Timeout: timeout,
		Skip:    cfg.Behavior.SmokeTestSkip,
	}
}

// checkSystemResources warns on stderr if the 1-minute load average exceeds
// threshold * numCPU. Does nothing when WarnOnSystemResources is false.
func checkSystemResources(cfg *config.Config, w io.Writer) {
	if !cfg.Behavior.WarnOnSystemResources {
		return
	}
	load := sysinfo.LoadAvg1()
	if load == 0 {
		return
	}
	ncpu := float64(runtime.NumCPU())
	threshold := cfg.Behavior.SystemLoadThreshold * ncpu
	if load > threshold {
		_, _ = fmt.Fprintf(w, "warning: system load is high (%.2f, threshold %.2f) — inference may be slow\n", load, threshold)
	}
}
