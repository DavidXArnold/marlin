package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/doctor"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run environment health checks",
	Long: `doctor checks your marlin environment for common problems.

Use --fix to automatically apply safe fixes (file permissions, missing directories).
Use --yes to skip confirmation prompts when applying fixes.`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().Bool("fix", false, "apply CanFix fixes automatically")
	doctorCmd.Flags().Bool("yes", false, "skip confirmation when applying fixes")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	fix, _ := cmd.Flags().GetBool("fix")
	yes, _ := cmd.Flags().GetBool("yes")

	checks := doctor.AllChecks(cfg, cfgFile)

	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()

	var (
		passCount int
		warnCount int
		failCount int
	)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	for _, chk := range checks {
		result := chk.Run(ctx)
		if result.ID == "" {
			result.ID = chk.ID()
		}

		switch result.Level {
		case doctor.LevelPass:
			passCount++
		case doctor.LevelWarn:
			warnCount++
		case doctor.LevelFail:
			failCount++
		}

		_, _ = fmt.Fprintf(w, "[%s] %-28s %s\n", result.Level, result.ID, result.Detail)
		if (result.Level == doctor.LevelWarn || result.Level == doctor.LevelFail) && result.Hint != "" {
			_, _ = fmt.Fprintf(w, "       hint: %s\n", result.Hint)
		}

		if fix && result.CanFix {
			if !yes {
				ok := confirmPrompt(w, cmd.InOrStdin(),
					fmt.Sprintf("       fix %s? [y/N] ", result.ID))
				if !ok {
					continue
				}
			}
			if fixErr := chk.Fix(ctx); fixErr != nil {
				_, _ = fmt.Fprintf(errW, "       fix failed: %v\n", fixErr)
			} else {
				_, _ = fmt.Fprintf(w, "       fixed\n")
			}
		}
	}

	_, _ = fmt.Fprintf(w, "\n%d PASS, %d WARN, %d FAIL\n", passCount, warnCount, failCount)

	if failCount > 0 {
		return fmt.Errorf("%s", strings.Repeat("", 0)+"doctor found failures")
	}
	return nil
}
