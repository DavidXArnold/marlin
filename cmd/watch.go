package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/vllm"
	"github.com/DavidXArnold/marlin/internal/watchdog"
)

var watchCmd = &cobra.Command{
	Use:   "watch [model]",
	Short: "Watch the active model and restart it if the API becomes unhealthy",
	Long: `Poll the inference API health endpoint and automatically restart the model
when it stops responding.

Without a model argument, watches whatever is currently active in marlin's state.
Exits with a non-zero status when max-restarts is exceeded or a restart fails.

Examples:
  marlin watch                          # watch active model
  marlin watch qwen3-32b-nvfp4         # watch a specific model
  marlin watch --interval 1m --max-restarts 0  # unlimited restarts, check every minute`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatch,
}

// watchRunFunc is injectable for tests.
var watchRunFunc = watchdog.Run

func init() {
	rootCmd.AddCommand(watchCmd)
	watchCmd.Flags().Duration("interval", 30*time.Second, "Health check interval")
	watchCmd.Flags().Int("max-restarts", 5, "Max restarts in restart-window before giving up (0 = unlimited)")
	watchCmd.Flags().Duration("restart-window", 10*time.Minute, "Rolling window for counting restarts")
	watchCmd.Flags().Duration("restart-delay", 5*time.Second, "Pause between failure detection and restart")
}

func runWatch(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, err := state.Load(cfg.Paths.StateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	slug := cur.ActiveModel
	if len(args) > 0 {
		slug = args[0]
	}
	if slug == "" {
		return fmt.Errorf("no active model — run 'marlin switch' first or pass a model slug")
	}

	p, err := buildProvider(cur.ActiveProvider, cfg)
	if err != nil {
		return fmt.Errorf("building provider: %w", err)
	}

	interval, _ := cmd.Flags().GetDuration("interval")
	maxRestarts, _ := cmd.Flags().GetInt("max-restarts")
	restartWindow, _ := cmd.Flags().GetDuration("restart-window")
	restartDelay, _ := cmd.Flags().GetDuration("restart-delay")

	wcfg := watchdog.Config{
		Interval:      interval,
		MaxRestarts:   maxRestarts,
		RestartWindow: restartWindow,
		RestartDelay:  restartDelay,
	}

	activeM, _ := config.ResolveModel(slug, effectiveDirs(cfg)...)
	primaryPath := config.EffectiveHealthPath(activeM, cfg.Server.HealthPath)
	probePaths := append([]string{primaryPath}, vllm.KnownHealthPaths...)
	client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "", primaryPath)
	isHealthy := func(ctx context.Context) bool {
		_, ready := client.HealthProbe(ctx, probePaths...)
		return ready
	}
	restart := func(ctx context.Context) error {
		return p.Switch(ctx, slug)
	}

	return watchRunFunc(cmd.Context(), wcfg, isHealthy, slug, restart, cmd.OutOrStdout())
}
