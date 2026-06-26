package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/bench"
	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/vllm"
)

const defaultBenchPrompt = "Describe the water cycle in exactly 50 words."

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Benchmark the active model: TTFT and decode throughput",
	Args:  cobra.NoArgs,
	RunE:  runBench,
}

func init() {
	rootCmd.AddCommand(benchCmd)
	benchCmd.Flags().String("prompt", defaultBenchPrompt, "prompt to send")
	benchCmd.Flags().Int("max-tokens", 256, "max output tokens per run")
	benchCmd.Flags().Int("runs", 3, "number of benchmark iterations")
}

// benchRunFunc is injectable for tests.
var benchRunFunc = defaultRunBench

func runBench(cmd *cobra.Command, args []string) error {
	return benchRunFunc(cmd, args)
}

func defaultRunBench(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	prompt, _ := cmd.Flags().GetString("prompt")
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	runs, _ := cmd.Flags().GetInt("runs")
	if runs < 1 {
		return fmt.Errorf("--runs must be >= 1")
	}

	cur, _ := state.Load(cfg.Paths.StateFile)
	activeM, _ := config.ResolveModel(cur.ActiveModel, effectiveDirs(cfg)...)
	client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "", config.EffectiveHealthPath(activeM, cfg.Server.HealthPath))

	// Verify the model is up before benchmarking.
	health, err := client.Health(cmd.Context())
	if err != nil || !health.Ready {
		return fmt.Errorf("model not ready — start a model with 'marlin switch' first")
	}

	models, err := client.Models(cmd.Context())
	if err != nil || len(models) == 0 {
		return fmt.Errorf("no models available at %s:%d", cfg.Server.Host, cfg.Server.Port)
	}
	model := models[0].ID

	// Adapter: wrap vllm.Client.ChatStream to match bench.StreamFn.
	stream := func(ctx context.Context, m, p string, mt int, fn func(bench.TokenEvent) error) error {
		return client.ChatStream(ctx, m, p, mt, func(sc vllm.StreamChunk) error {
			return fn(bench.TokenEvent{Content: sc.Content, FinishReason: sc.FinishReason})
		})
	}

	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "benchmarking %s (%d run(s))…\n\n", model, runs)

	var results []*bench.Result
	for i := range runs {
		_, _ = fmt.Fprintf(w, "run %d/%d… ", i+1, runs)
		r, runErr := bench.Run(cmd.Context(), stream, model, prompt, maxTokens)
		if runErr != nil {
			return fmt.Errorf("run %d: %w", i+1, runErr)
		}
		results = append(results, r)
		_, _ = fmt.Fprintf(w, "TTFT %s  tok/s %.1f\n",
			r.TTFT.Round(1*1000*1000), r.DecodeToksPerSec)
	}

	_, _ = fmt.Fprintln(w)
	bench.Print(w, bench.Summarise(results), model)
	return nil
}
