package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/vllm"
)

// startMaxRuntimeTimerFunc is injectable for tests.
var startMaxRuntimeTimerFunc = maxRuntimeTimer

// maxRuntimeTimer blocks until d elapses or the command context is cancelled,
// then calls p.Stop to shut down the running model.
func maxRuntimeTimer(cmd *cobra.Command, _ *config.Config, slug string, p provider.Provider, d time.Duration) {
	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "max-runtime: %s will stop automatically in %s\n", slug, d)
	select {
	case <-cmd.Context().Done():
		return
	case <-time.After(d):
	}
	_, _ = fmt.Fprintf(w, "max-runtime reached — stopping %s\n", slug)
	if err := p.Stop(cmd.Context()); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error stopping %s: %v\n", slug, err)
	}
}

var startCmd = &cobra.Command{
	Use:   "start [model]",
	Short: "Start the inference service, optionally selecting a model",
	Long: `Start the inference service.

Without arguments: if a model is already active and the service is stopped,
it is restarted without switching models. If no model has been activated yet,
an interactive picker lets you choose one.

With a model name: behaves like 'marlin switch <model>'.

Use --enable to also configure the systemd unit to start automatically at boot.
Use --logs to stream container/service logs while waiting for the API to become ready.
Use -vvv to include container logs on stderr alongside the progress indicator.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

// enableUnit is injectable for tests.
var enableUnit = func(cfg *config.Config) error {
	svc := service.NewSystemdManager(cfg.Service.SystemdUnit)
	if err := svc.Enable(rootCmd.Context()); err != nil {
		return fmt.Errorf("enabling %s at boot: %w", cfg.Service.SystemdUnit, err)
	}
	return nil
}

// startWaitForReadyFunc is injectable for tests.
// Returns true when the API became ready, false on timeout or container exit.
var startWaitForReadyFunc = waitForReady

// startLogsPromptReader is injectable for tests.
var startLogsPromptReader io.Reader = os.Stdin

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// startWaitTimeout caps how long we poll the health endpoint after a start.
// Declared as var so tests can shorten it.
var startWaitTimeout = 10 * time.Minute

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().Bool("enable", false, "Also enable the systemd unit to start at boot")
	startCmd.Flags().BoolP("logs", "l", false, "Stream container/service logs while waiting for the API")
	startCmd.Flags().String("max-runtime", "", "Stop the model after this duration (e.g. 15m, 1h); 0 = disabled")
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	enable, _ := cmd.Flags().GetBool("enable")

	// Switch (or restart) the active model — launches the service.
	if err := runSwitch(cmd, args); err != nil {
		return err
	}

	// Post-switch: wait for the API to become ready, showing progress.
	cur, _ := state.Load(cfg.Paths.StateFile)
	p, buildErr := buildProvider(cur.ActiveProvider, cfg)
	if buildErr == nil {
		ok := startWaitForReadyFunc(cmd, cfg, cur.ActiveModel, p)
		if !ok && stdoutIsTerminal() {
			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "show logs? [y/N] ")
			buf := make([]byte, 4)
			n, _ := startLogsPromptReader.Read(buf)
			if strings.ToLower(strings.TrimSpace(string(buf[:n]))) == "y" {
				if err := p.Logs(cmd.Context(), w, false, 100); err != nil {
					_, _ = fmt.Fprintf(w, "could not fetch logs: %v\n", err)
				}
			}
		}
	}

	if enable {
		if err := enableUnit(cfg); err != nil {
			return err
		}
	}

	// Block and auto-stop if --max-runtime / behavior.max_runtime is set.
	if buildErr == nil {
		if maxRT := effectiveMaxRuntime(cmd, cfg); maxRT > 0 {
			startMaxRuntimeTimerFunc(cmd, cfg, cur.ActiveModel, p, maxRT)
		}
	}

	return nil
}

// waitForReady polls the OpenAI-compatible health endpoint after a start/switch,
// showing a spinner on stdout. When --logs is set, container logs stream to stdout.
// When -vvv is set, container logs stream to stderr alongside the spinner.
// Returns true when the API is ready, false on timeout or container exit.
func waitForReady(cmd *cobra.Command, cfg *config.Config, slug string, p provider.Provider) bool {
	streamLogs, _ := cmd.Flags().GetBool("logs")
	showLogs := streamLogs || Verbosity >= 3

	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()

	ctx, cancel := context.WithTimeout(cmd.Context(), startWaitTimeout)
	defer cancel()

	client := vllm.NewClient(cfg.Server.Host, cfg.Server.Port, "")

	// Fast path: already ready (e.g., vLLM process already healthy after systemd restart).
	if h, err := client.Health(ctx); err == nil && h.Ready {
		_, _ = fmt.Fprintf(w, "api ready at http://%s:%d/v1\n", cfg.Server.Host, cfg.Server.Port)
		return true
	}

	// Stream logs concurrently if requested.
	if showLogs {
		logCtx, logCancel := context.WithCancel(ctx)
		defer logCancel()
		logDst := errW // -vvv → stderr
		if streamLogs {
			logDst = w // --logs → stdout
		}
		go func() { _ = p.Logs(logCtx, logDst, true, 0) }()
	}

	// Use in-place spinner only when stdout is a TTY and logs aren't streaming there.
	isTTY := !streamLogs && stdoutIsTerminal()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	start := time.Now()
	frame := 0

	if !isTTY && !showLogs {
		_, _ = fmt.Fprintf(w, "waiting for %s ...\n", slug)
	}

	for {
		select {
		case <-ctx.Done():
			if isTTY {
				_, _ = fmt.Fprintf(w, "\r\033[K") // clear spinner line
			}
			_, _ = fmt.Fprintf(w, "start timed out after %s — run 'marlin logs' for details\n",
				time.Since(start).Round(time.Second))
			return false

		case <-ticker.C:
			if h, err := client.Health(cmd.Context()); err == nil && h.Ready {
				if isTTY {
					_, _ = fmt.Fprintf(w, "\r\033[K") // clear spinner line
				}
				_, _ = fmt.Fprintf(w, "ready at http://%s:%d/v1 (%s)\n",
					cfg.Server.Host, cfg.Server.Port,
					time.Since(start).Round(time.Second))
				return true
			}

			// Detect container exit so we don't spin indefinitely.
			if status, err := p.Status(cmd.Context()); err == nil {
				if cs := status.ContainerState; cs != "" && cs != "running" {
					if isTTY {
						_, _ = fmt.Fprintf(w, "\r\033[K")
					}
					_, _ = fmt.Fprintf(w, "container exited (%s) after %s — run 'marlin logs' for details\n",
						cs, time.Since(start).Round(time.Second))
					return false
				}
			}

			if isTTY {
				elapsed := time.Since(start).Round(time.Second)
				_, _ = fmt.Fprintf(w, "\r%s %s ... %s   ",
					spinFrames[frame%len(spinFrames)], slug, elapsed)
				frame++
			}
		}
	}
}
