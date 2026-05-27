package cmd

import (
	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/state"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail inference service logs",
	RunE:  runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	logsCmd.Flags().Int("lines", 100, "Number of lines to show")
}

func runLogs(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)
	if cur.ActiveProvider == "" {
		_ = cur // no active provider, default to vllm
	}

	p, err := buildProvider(cur.ActiveProvider, cfg)
	if err != nil {
		return err
	}

	follow, _ := cmd.Flags().GetBool("follow")
	lines, _ := cmd.Flags().GetInt("lines")

	return p.Logs(cmd.Context(), cmd.OutOrStdout(), follow, lines)
}
