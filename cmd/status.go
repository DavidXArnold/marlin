package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
	"github.com/DavidXArnold/marlin/internal/vllm"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current model, service state, and hardware resources",
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

	if cur.ActiveModel != "" {
		fmt.Fprintf(w, "active model : %s\n", cur.ActiveModel)
		fmt.Fprintf(w, "provider     : %s\n", cur.ActiveProvider)
		if cur.ContainerID != "" {
			fmt.Fprintf(w, "container    : %s\n", cur.ContainerID[:min12(len(cur.ContainerID))])
		}

		client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "")
		health, err := client.Health(cmd.Context())
		if err != nil {
			fmt.Fprintf(w, "api health   : error (%v)\n", err)
		} else if health.Ready {
			fmt.Fprintf(w, "api health   : ready at http://%s:%d/v1\n", cfg.Server.Host, cfg.Server.Port)
		} else {
			fmt.Fprintf(w, "api health   : not ready\n")
		}
	} else {
		fmt.Fprintln(w, "no active model")
	}

	// Hardware section — always shown, failures are soft.
	fmt.Fprintln(w)
	si, err := sysinfo.Detect(cfg.Paths.ModelsDir, cfg.Paths.NIMCache)
	if err != nil {
		fmt.Fprintf(w, "hardware     : detection error (%v)\n", err)
		return nil
	}

	if len(si.GPUs) == 0 {
		fmt.Fprintln(w, "gpu          : none detected (nvidia-smi not found)")
	} else {
		for _, g := range si.GPUs {
			fmt.Fprintf(w, "gpu[%d]       : %s  vram %s free / %s total\n",
				g.Index, g.Name,
				sysinfo.FormatMB(g.VRAMFreeMB),
				sysinfo.FormatMB(g.VRAMTotalMB))
		}
	}

	if si.RAMTotalMB > 0 {
		fmt.Fprintf(w, "ram          : %s free / %s total\n",
			sysinfo.FormatMB(si.RAMFreeMB),
			sysinfo.FormatMB(si.RAMTotalMB))
	}

	for path, d := range si.Disks {
		fmt.Fprintf(w, "disk %-8s: %.1f GiB free / %.1f GiB total\n",
			diskLabel(path, cfg.Paths.ModelsDir, cfg.Paths.NIMCache),
			d.FreeGB, d.TotalGB)
	}

	return nil
}

func diskLabel(path, modelsDir, nimCache string) string {
	switch path {
	case modelsDir:
		return "(models)"
	case nimCache:
		return "(nim cache)"
	default:
		return ""
	}
}

func min12(n int) int {
	if n < 12 {
		return n
	}
	return 12
}
