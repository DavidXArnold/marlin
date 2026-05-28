package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/validate"
)

var validateCmd = &cobra.Command{
	Use:   "validate <model>",
	Short: "Check a model config for common issues before switching",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	slug := args[0]
	m, err := config.LoadModel(filepath.Join(cfg.Paths.ModelsDir, slug+".toml"))
	if err != nil {
		return fmt.Errorf("loading model %q: %w", slug, err)
	}

	issues := validate.Model(m, cfg.Server.Alias)

	w := cmd.OutOrStdout()
	if len(issues) == 0 {
		_, err := fmt.Fprintf(w, "%s: OK\n", slug)
		return err
	}

	for _, iss := range issues {
		if _, err := fmt.Fprintf(w, "[%s] %s\n", iss.Level, iss.Message); err != nil {
			return err
		}
	}

	return nil
}
