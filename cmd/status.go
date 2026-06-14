package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
	"github.com/DavidXArnold/marlin/internal/vllm"
)

// newStatusSystemdManager is injectable for tests.
var newStatusSystemdManager = service.NewSystemdManager

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

		// Live status from the provider (works for all provider types).
		var liveStatus *provider.Status
		if p, err := buildProvider(cur.ActiveProvider, cfg); err == nil {
			liveStatus, _ = p.Status(cmd.Context())
		}

		if liveStatus != nil && liveStatus.ContainerState != "" {
			// Container provider (NIM): show container ID and state.
			id := liveStatus.ContainerID
			if len(id) > 12 {
				id = id[:12]
			}
			var containerLine string
			if id != "" {
				containerLine = fmt.Sprintf("%s  (%s)", id, liveStatus.ContainerState)
			} else {
				containerLine = liveStatus.ContainerState
			}
			if err := writef("container    : %s\n", containerLine); err != nil {
				return err
			}
		} else if liveStatus != nil {
			// Service provider (vLLM): show running/stopped.
			svcState := "stopped"
			if liveStatus.Running {
				svcState = "running"
			}
			if err := writef("service      : %s\n", svcState); err != nil {
				return err
			}
		} else if cur.ContainerID != "" {
			// Fallback: cached container ID from state.
			if err := writef("container    : %s\n", cur.ContainerID[:min12(len(cur.ContainerID))]); err != nil {
				return err
			}
		}

		// Show stop time when deliberately stopped via marlin stop and not yet running.
		notRunning := liveStatus == nil || !liveStatus.Running
		if cur.StoppedAt != nil && notRunning {
			if err := writef("last stop    : %s ago\n", humanDuration(time.Since(*cur.StoppedAt))); err != nil {
				return err
			}
		}

		// Skip API health check only when deliberately stopped (StoppedAt set) AND
		// the service is confirmed not running. For crashed containers we still show
		// the API state and last log lines to help diagnose the failure.
		deliberatelyStopped := cur.StoppedAt != nil && notRunning
		if !deliberatelyStopped {
			client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "", cfg.Server.HealthPath)
			health, healthErr := client.Health(cmd.Context())
			apiReady := healthErr == nil && health.Ready
			if healthErr != nil {
				if err := writef("api health   : error (%v)\n", healthErr); err != nil {
					return err
				}
			} else if apiReady {
				if err := writef("api health   : ready at http://%s:%d/v1\n", cfg.Server.Host, cfg.Server.Port); err != nil {
					return err
				}
			} else {
				if err := writef("api health   : not ready\n"); err != nil {
					return err
				}
				// For container providers, show the last log lines to explain why.
				if cur.ActiveProvider == config.ProviderNIM {
					if p, err := buildProvider(cur.ActiveProvider, cfg); err == nil {
						var logBuf bytes.Buffer
						if err := p.Logs(cmd.Context(), &logBuf, false, 10); err == nil {
							logs := logBuf.String()
							for i, line := range lastNLines(logs, 2) {
								label := "last log     "
								if i > 0 {
									label = "             "
								}
								if err := writef("%s: %s\n", label, line); err != nil {
									return err
								}
							}
							if hint := nimHint(logs); hint != "" {
								if err := writef("hint         : %s\n", hint); err != nil {
									return err
								}
							}
						}
					}
				}
			}
		}

		// Show systemd boot-enable status for vLLM units to surface the feature.
		if cur.ActiveProvider == config.ProviderVLLM || cur.ActiveProvider == "" {
			svc := newStatusSystemdManager(cfg.Service.SystemdUnit)
			if enabled, err := svc.IsEnabled(cmd.Context()); err == nil {
				bootLine := "enabled"
				if !enabled {
					bootLine = "disabled  (run 'marlin start --enable' to start at boot)"
				}
				if err := writef("boot         : %s\n", bootLine); err != nil {
					return err
				}
			}
		}
	} else {
		if err := writeln("no active model"); err != nil {
			return err
		}
	}

	// Ad-hoc containers section — show marlin-managed containers started via marlin run.
	if adhocR, err := buildAdhocRunner(cfg); err == nil {
		if infos, err := adhocR.List(cmd.Context()); err == nil && len(infos) > 0 {
			if err := writeln(); err != nil {
				return err
			}
			if err := writeln("ad-hoc containers:"); err != nil {
				return err
			}
			for _, info := range infos {
				portStr := ""
				if info.Port != "" {
					portStr = "  :" + info.Port
				}
				id := info.ID
				if len(id) > 12 {
					id = id[:12]
				}
				if err := writef("  %-20s  %s%s  (%s)  run 'marlin logs %s' to inspect\n",
					info.Slug, info.Status, portStr, id, info.Slug); err != nil {
					return err
				}
			}
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
		nimActive := cur.ActiveProvider == config.ProviderNIM
		var sawUMA bool
		for _, g := range si.GPUs {
			var gpuLine string
			if g.IsUMA {
				sawUMA = true
				cc := ""
				if g.ComputeCap != "" {
					cc = "  sm_" + strings.ReplaceAll(g.ComputeCap, ".", "")
				}
				gpuLine = fmt.Sprintf("gpu[%d]       : %s  unified memory (see RAM)%s\n", g.Index, g.Name, cc)
			} else {
				gpuLine = fmt.Sprintf("gpu[%d]       : %s  vram %s free / %s total\n",
					g.Index, g.Name,
					sysinfo.FormatMB(g.VRAMFreeMB),
					sysinfo.FormatMB(g.VRAMTotalMB))
			}
			if err := writef("%s", gpuLine); err != nil {
				return err
			}
		}
		if sawUMA && nimActive {
			if err := writef("               hint: add extra_env = [\"NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9\"] if model OOMs\n"); err != nil {
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

func nimHint(logs string) string {
	lower := strings.ToLower(logs)
	if strings.Contains(lower, "uma device detected") || strings.Contains(lower, "no available memory") {
		return `UMA/unified memory detected — try: extra_env = ["NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9"]`
	}
	if strings.Contains(lower, "out of memory") || strings.Contains(lower, "cuda out of memory") {
		return `GPU OOM — try reducing load: extra_env = ["NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.7"]`
	}
	return ""
}

func lastNonEmptyLine(s string) string {
	lines := lastNLines(s, 1)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// lastNLines returns up to n non-empty lines from the tail of s, in chronological order.
func lastNLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var result []string
	for i := len(lines) - 1; i >= 0 && len(result) < n; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			result = append(result, t)
		}
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
