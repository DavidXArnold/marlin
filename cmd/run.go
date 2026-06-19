package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
)

// adhocRunner is the subset of provider.AdhocRunner used by run/ps/stop/status commands.
// Declared as an interface so tests can inject a stub.
type adhocRunner interface {
	Start(ctx context.Context, slug string) (string, error)
	RunForeground(ctx context.Context, slug string, w io.Writer) error
	LogsFor(ctx context.Context, slug string, w io.Writer, follow bool, lines int) error
	List(ctx context.Context) ([]provider.AdhocInfo, error)
	Stop(ctx context.Context, slug string) error
	StopAll(ctx context.Context) error
	DetectUnmanaged(ctx context.Context) ([]provider.UnmanagedContainer, error)
}

// injectable for tests
var buildAdhocRunner = func(cfg *config.Config) (adhocRunner, error) {
	return provider.NewAdhocRunner(cfg)
}

var runCmd = &cobra.Command{
	Use:   "run <model>",
	Short: "Run a model ad-hoc in a Docker container (no systemd required)",
	Long: `Pull and start a model container without installing a managed service.
The container is labelled so marlin can list and stop it later.

Foreground mode (default) streams logs and removes the container on exit.
Use --detach to start in the background and manage with marlin ps / marlin stop.`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().BoolP("detach", "d", false, "Start in background and return immediately")
	runCmd.Flags().String("max-runtime", "", "Stop the container after this duration (e.g. 15m, 1h); 0 = disabled")
}

func runRun(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	slug := args[0]
	detach, _ := cmd.Flags().GetBool("detach")
	maxRT := effectiveMaxRuntime(cmd, cfg)

	runner, err := buildAdhocRunner(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner: %w", err)
	}

	w := cmd.OutOrStdout()

	if detach {
		if maxRT > 0 {
			_, _ = fmt.Fprintf(w, "warning: --max-runtime is not supported in detach mode\n")
		}
		id, err := runner.Start(cmd.Context(), slug)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "started %s (container %s)\n", slug, shortID(id)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "use 'marlin ps' to list running containers"); err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "use 'marlin stop %s' to stop it\n", slug)
		return err
	}

	// Foreground: catch interrupt so cleanup deferred in RunForeground fires.
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if maxRT > 0 {
		var rtCancel context.CancelFunc
		ctx, rtCancel = context.WithTimeout(ctx, maxRT)
		defer rtCancel()
		_, _ = fmt.Fprintf(w, "max-runtime: container will stop after %s\n", maxRT)
	}

	if _, err := fmt.Fprintf(w, "running %s (Ctrl-C to stop and remove)\n", slug); err != nil {
		return err
	}
	return runner.RunForeground(ctx, slug, w)
}
