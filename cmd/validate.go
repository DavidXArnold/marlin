package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/validate"
	"github.com/DavidXArnold/marlin/pkg/render"
)

var validateCmd = &cobra.Command{
	Use:   "validate <model>",
	Short: "Check a model config for common issues before switching",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
	validateCmd.Flags().Bool("show-config", false, "print the fully rendered config (env file, systemd unit or docker run)")
}

func runValidate(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	slug := args[0]
	m, err := config.ResolveModel(slug, effectiveDirs(cfg)...)
	if err != nil {
		return fmt.Errorf("model %q: %w", slug, err)
	}

	issues := validate.Model(m, cfg.Server.Alias)

	w := cmd.OutOrStdout()
	if len(issues) == 0 {
		if _, err := fmt.Fprintf(w, "%s: OK\n", slug); err != nil {
			return err
		}
	} else {
		for _, iss := range issues {
			if _, err := fmt.Fprintf(w, "[%s] %s\n", iss.Level, iss.Message); err != nil {
				return err
			}
		}
	}

	showConfig, _ := cmd.Flags().GetBool("show-config")
	if showConfig {
		_, err = fmt.Fprint(w, render.Inspect(m, cfg))
	}
	return err
}
