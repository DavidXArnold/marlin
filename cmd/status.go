package cmd

import (
	"fmt"
	"strings"

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
	writef := func(format string, args ...any) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	writeln := func(args ...any) error {
		_, err := fmt.Fprintln(w, args...)
		return err
	}

	if cur.ActiveModel != "" {
		if err := writef("active model : %s\n", cur.ActiveModel); err != nil {
			return err
		}
		if err := writef("provider     : %s\n", cur.ActiveProvider); err != nil {
			return err
		}
		if cur.ContainerID != "" {
			if err := writef("container    : %s\n", cur.ContainerID[:min12(len(cur.ContainerID))]); err != nil {
				return err
			}
		}

		client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "")
		health, err := client.Health(cmd.Context())
		if err != nil {
			if err := writef("api health   : error (%v)\n", err); err != nil {
				return err
			}
		} else if health.Ready {
			if err := writef("api health   : ready at http://%s:%d/v1\n", cfg.Server.Host, cfg.Server.Port); err != nil {
				return err
			}
		} else {
			if err := writef("api health   : not ready\n"); err != nil {
				return err
			}
		}
	} else {
		if err := writeln("no active model"); err != nil {
			return err
		}
	}

	// Hardware section — always shown, failures are soft.
	if err := writeln(); err != nil {
		return err
	}
	si, err := sysinfo.Detect(cfg.Paths.ModelsDir, cfg.Paths.NIMCache)
	if err != nil {
		return writef("hardware     : detection error (%v)\n", err)
	}

	if len(si.GPUs) == 0 {
		if err := writeln("gpu          : none detected (nvidia-smi not found)"); err != nil {
			return err
		}
	} else {
		for _, g := range si.GPUs {
			if err := writef("gpu[%d]       : %s  vram %s free / %s total\n",
				g.Index, g.Name,
				sysinfo.FormatMB(g.VRAMFreeMB),
				sysinfo.FormatMB(g.VRAMTotalMB)); err != nil {
				return err
			}
		}
	}

	if si.RAMTotalMB > 0 {
		if err := writef("ram          : %s free / %s total\n",
			sysinfo.FormatMB(si.RAMFreeMB),
			sysinfo.FormatMB(si.RAMTotalMB)); err != nil {
			return err
		}
	}

	for path, d := range si.Disks {
		if err := writef("disk %-11s: %.1f GiB free / %.1f GiB total\n",
			diskLabel(path, cfg.Paths.ModelsDir, cfg.Paths.NIMCache),
			d.FreeGB, d.TotalGB); err != nil {
			return err
		}
	}

	// Unmanaged container warning — soft failure, configurable.
	if cfg.Behavior.WarnUnmanagedContainers {
		runner, err := buildAdhocRunner(cfg)
		if err == nil {
			if unmanaged, err := runner.DetectUnmanaged(cmd.Context()); err == nil && len(unmanaged) > 0 {
				if err := writeln(); err != nil {
					return err
				}
				if err := writeln("warning: unmanaged inference containers detected (not started by marlin):"); err != nil {
					return err
				}
				for _, c := range unmanaged {
					name := strings.Join(c.Names, ", ")
					id := c.ID
					if len(id) > 12 {
						id = id[:12]
					}
					if err := writef("  %s  image: %s  name: %s\n", id, c.Image, name); err != nil {
						return err
					}
				}
				if err := writeln("  use 'marlin run' to manage containers, or set warn_unmanaged_containers=false to silence"); err != nil {
					return err
				}
			}
		}
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
