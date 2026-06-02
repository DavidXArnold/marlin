package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/secrets"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
	"github.com/DavidXArnold/marlin/internal/ui"
)

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
		return provider.NewVLLMProvider(cfg), nil
	case config.ProviderNIM:
		sec, err := secrets.Load(cfg.Paths.SecretsEnv)
		if err != nil {
			return nil, fmt.Errorf("loading secrets: %w", err)
		}
		if cfg.Service.ContainerRuntime == "containerd" {
			return provider.NewContainerdNIMProvider(cfg, sec["NGC_API_KEY"])
		}
		return provider.NewNIMProvider(cfg, sec["NGC_API_KEY"])
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
func resolveModel(query string, names []string, cfgs []*config.ModelConfig, activeSlug string) (string, error) {
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

	return ui.PickModel(names, cfgs, "", activeSlug)
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
