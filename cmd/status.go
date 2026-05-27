package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/vllm"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current model, service state, and token throughput",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	w := cmd.OutOrStdout()

	if cur.ActiveModel == "" {
		fmt.Fprintln(w, "no active model")
		return nil
	}

	fmt.Fprintf(w, "active model : %s\n", cur.ActiveModel)
	fmt.Fprintf(w, "provider     : %s\n", cur.ActiveProvider)
	if cur.ContainerID != "" {
		fmt.Fprintf(w, "container    : %s\n", cur.ContainerID[:min12(len(cur.ContainerID))])
	}

	// Live health via the vLLM / NIM OpenAI-compatible API.
	client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "")
	health, err := client.Health(cmd.Context())
	if err != nil {
		fmt.Fprintf(w, "api health   : error (%v)\n", err)
	} else if health.Ready {
		fmt.Fprintf(w, "api health   : ready at http://%s:%d/v1\n", cfg.Server.Host, cfg.Server.Port)
	} else {
		fmt.Fprintf(w, "api health   : not ready\n")
	}

	return nil
}

func min12(n int) int {
	if n < 12 {
		return n
	}
	return 12
}
