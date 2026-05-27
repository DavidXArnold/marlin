package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/registry"
	"github.com/DavidXArnold/marlin/internal/secrets"
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
		fmt.Fprintf(w, "%-60s %s\n", "ID", "DESCRIPTION")
		fmt.Fprintf(w, "%-60s %s\n", "--", "-----------")
		for _, m := range results {
			desc := m.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Fprintf(w, "%-60s %s\n", m.ID, desc)
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
