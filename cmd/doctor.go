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

type doctorResultJSON struct {
	Level  string `json:"level"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
	Fixed  bool   `json:"fixed,omitempty"`
}

type doctorOutputJSON struct {
	Results []doctorResultJSON `json:"results"`
	Pass    int                `json:"pass"`
	Warn    int                `json:"warn"`
	Fail    int                `json:"fail"`
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

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	isStructured := outputFormat == "json" || outputFormat == "jsonl" || outputFormat == "plain"

	var (
		passCount int
		warnCount int
		failCount int
		jsonItems []doctorResultJSON
	)

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

		fixed := false
		if fix && result.CanFix {
			shouldFix := yes || isStructured // non-interactive formats auto-apply
			if !shouldFix {
				ok := confirmPrompt(w, cmd.InOrStdin(),
					fmt.Sprintf("       fix %s? [y/N] ", result.ID))
				shouldFix = ok
			}
			if shouldFix {
				if fixErr := chk.Fix(ctx); fixErr != nil {
					_, _ = fmt.Fprintf(errW, "       fix failed: %v\n", fixErr)
				} else {
					fixed = true
				}
			}
		}

		if isStructured {
			jsonItems = append(jsonItems, doctorResultJSON{
				Level:  string(result.Level),
				ID:     result.ID,
				Detail: result.Detail,
				Hint:   result.Hint,
				Fixed:  fixed,
			})
			continue
		}

		// table output: print as we go
		_, _ = fmt.Fprintf(w, "[%s] %-28s %s\n", result.Level, result.ID, result.Detail)
		if (result.Level == doctor.LevelWarn || result.Level == doctor.LevelFail) && result.Hint != "" {
			_, _ = fmt.Fprintf(w, "       hint: %s\n", result.Hint)
		}
		if fixed {
			_, _ = fmt.Fprintf(w, "       fixed\n")
		}
	}

	switch outputFormat {
	case "json":
		return writeJSON(w, doctorOutputJSON{
			Results: jsonItems,
			Pass:    passCount,
			Warn:    warnCount,
			Fail:    failCount,
		})
	case "jsonl":
		for _, it := range jsonItems {
			if err := writeJSONLine(w, it); err != nil {
				return err
			}
		}
		return nil
	case "plain":
		for _, it := range jsonItems {
			_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", it.Level, it.ID, it.Detail)
			if err != nil {
				return err
			}
		}
		return nil
	default: // table
		_, _ = fmt.Fprintf(w, "\n%d PASS, %d WARN, %d FAIL\n", passCount, warnCount, failCount)
	}

	if failCount > 0 {
		return fmt.Errorf("%s", strings.Repeat("", 0)+"doctor found failures")
	}
	return nil
}
