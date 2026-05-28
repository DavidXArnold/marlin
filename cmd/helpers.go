package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/secrets"
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

// resolveModel resolves a user-supplied name (exact, fuzzy, or empty) to a
// model slug. Launches the interactive picker when the match is ambiguous or
// the query is empty.
func resolveModel(query string, names []string, cfgs []*config.ModelConfig) (string, error) {
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
		// Multiple fuzzy matches — show picker pre-filtered.
		return ui.PickModel(matches, nil, query)
	}

	return ui.PickModel(names, cfgs, "")
}
