package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/ui"
)

// injectable for tests
var (
	rmConfirmFunc   = ui.Confirm
	rmMultiPickFunc = func(names []string, cfgs []*config.ModelConfig, active string, history map[string]time.Time) ([]string, error) {
		return ui.MultiPickModel(names, cfgs, active, history)
	}
	isBundledFunc = config.IsBundled
)

var rmCmd = &cobra.Command{
	Use:   "rm [model...]",
	Short: "Remove one or more model profiles",
	Args:  cobra.ArbitraryArgs,
	RunE:  runRm,
}

func init() {
	rootCmd.AddCommand(rmCmd)
}

func runRm(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	dirs := effectiveDirs(cfg)
	models, names, err := config.ListModelsFromDirs(dirs...)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	var slugs []string
	if len(args) > 0 {
		nameSet := make(map[string]bool, len(names))
		for _, n := range names {
			nameSet[n] = true
		}
		for _, arg := range args {
			if !nameSet[arg] {
				return fmt.Errorf("model %q not found", arg)
			}
		}
		slugs = args
	} else {
		slugs, err = rmMultiPickFunc(names, models, cur.ActiveModel, cur.ModelHistory)
		if err != nil {
			return err
		}
	}

	if len(slugs) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "nothing selected")
		return nil
	}

	// Warn when any selected profile is a bundled default.
	var bundled []string
	for _, s := range slugs {
		if isBundledFunc(s) {
			bundled = append(bundled, s)
		}
	}
	if len(bundled) > 0 {
		verb := "is"
		if len(bundled) > 1 {
			verb = "are"
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"warning: %s %s a bundled default profile — it will be recreated on next install\n",
			strings.Join(bundled, ", "), verb)
	}

	ok, err := rmConfirmFunc(fmt.Sprintf("Remove %s?", strings.Join(slugs, ", ")))
	if err != nil {
		return err
	}
	if !ok {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "cancelled")
		return nil
	}

	for _, slug := range slugs {
		path, err := config.FindModelPath(slug, dirs...)
		if err != nil {
			return fmt.Errorf("model %q not found on disk", slug)
		}
		if err := privilege.PromptAndRemove(cmd.OutOrStdout(), path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
	}

	return nil
}
