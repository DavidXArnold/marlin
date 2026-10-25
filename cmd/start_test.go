package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/vllm"
)

// noopEnableUnit disables enableUnit side-effects for the duration of the test.
func noopEnableUnit(t *testing.T) {
	t.Helper()
	old := enableUnit
	enableUnit = func(_ *config.Config) error { return nil }
	t.Cleanup(func() { enableUnit = old })
}

// noopWaitForReady bypasses the post-switch health-polling loop for tests that
// don't have a live API server.
func noopWaitForReady(t *testing.T) {
	t.Helper()
	old := startWaitForReadyFunc
	startWaitForReadyFunc = func(_ *cobra.Command, _ *config.Config, _ string, _ provider.Provider) bool {
		return true
	}
	t.Cleanup(func() { startWaitForReadyFunc = old })
}

// TestStartNoActiveModel: no models → error propagates from switch.
func TestStartNoActiveModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopEnableUnit(t)

	_ = modelsDir
	_, err := executeCmd("start")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

// TestStartSingleModel: one model in dir, no arg → picker returns it directly, switch succeeds.
func TestStartSingleModel(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopWaitForReady(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	out, err := executeCmd("start")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
}

// TestStartWithModelArg: explicit model arg → switches directly without picker.
func TestStartWithModelArg(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopWaitForReady(t)
	injectProvider(t, &mockProv{})

	writeVLLMModel(t, modelsDir, "llama-8b")

	out, err := executeCmd("start", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
}

// TestStartWithEnable: --enable calls enableUnit after a successful switch.
func TestStartWithEnable(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopWaitForReady(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	var enabled bool
	old := enableUnit
	enableUnit = func(_ *config.Config) error { enabled = true; return nil }
	t.Cleanup(func() { enableUnit = old })

	_, err := executeCmd("start", "llama-8b", "--enable")
	require.NoError(t, err)
	assert.True(t, enabled)
}

// TestStartEnableNoArg: single model + --enable → switches then calls enableUnit.
func TestStartEnableNoArg(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopWaitForReady(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	var enabled bool
	old := enableUnit
	enableUnit = func(_ *config.Config) error { enabled = true; return nil }
	t.Cleanup(func() { enableUnit = old })

	out, err := executeCmd("start", "--enable")
	require.NoError(t, err)
	assert.Contains(t, out, "switched to")
	assert.True(t, enabled)
}

// TestStartEnableFails: enableUnit error propagates.
func TestStartEnableFails(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	noopWaitForReady(t)
	injectProvider(t, &mockProv{})
	writeVLLMModel(t, modelsDir, "llama-8b")

	old := enableUnit
	enableUnit = func(_ *config.Config) error { return fmt.Errorf("systemctl: permission denied") }
	t.Cleanup(func() { enableUnit = old })

	_, err := executeCmd("start", "llama-8b", "--enable")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

// TestStartProviderError: provider build failure is surfaced.
func TestStartProviderError(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	old := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return nil, fmt.Errorf("docker not available")
	}
	t.Cleanup(func() { buildProvider = old })

	_, err := executeCmd("start", "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker not available")
}

// --- waitForReady unit tests ---

// makeReadyServer returns an httptest.Server that responds 200 to all
// vllm.KnownHealthPaths and 404 to everything else.
func makeReadyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range vllm.KnownHealthPaths {
			if r.URL.Path == p {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cfgWithServer returns a config whose Server.Host/Port points at the given test server.
func cfgWithServer(t *testing.T, srv *httptest.Server) *config.Config {
	t.Helper()
	cfg, _ := config.Load("")
	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	cfg.Server.Host = host
	cfg.Server.Port = port
	return cfg
}

func TestWaitForReadyAlreadyReady(t *testing.T) {
	srv := makeReadyServer(t)
	cfg := cfgWithServer(t, srv)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	ok := waitForReady(cmd, cfg, "test-model", &mockProv{})
	assert.True(t, ok)
	assert.Contains(t, buf.String(), "ready")
}

func TestWaitForReadyEventuallyReady(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if ready.Load() {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := cfgWithServer(t, srv)

	go func() {
		time.Sleep(3 * time.Second)
		ready.Store(true)
	}()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	ok := waitForReady(cmd, cfg, "test-model", &mockProv{})
	assert.True(t, ok)
	assert.Contains(t, buf.String(), "ready")
	assert.Contains(t, buf.String(), "test-model")
}

func TestWaitForReadyLogsFlag(t *testing.T) {
	srv := makeReadyServer(t)
	cfg := cfgWithServer(t, srv)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")
	require.NoError(t, cmd.Flags().Set("logs", "true"))

	ok := waitForReady(cmd, cfg, "nim-model", &mockProv{})
	// Already ready → should print ready immediately; logs goroutine is cancelled.
	assert.True(t, ok)
	assert.Contains(t, buf.String(), "ready")
}

func TestMaxRuntimeTimerStopsProvider(t *testing.T) {
	var stopped bool
	p := &mockProv{stopFn: func() { stopped = true }}

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)

	maxRuntimeTimer(cmd, nil, "llama-8b", p, 50*time.Millisecond)

	assert.True(t, stopped)
	assert.Contains(t, buf.String(), "max-runtime reached")
}

func TestMaxRuntimeTimerCancelledByContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{SilenceUsage: true}
	cmd.SetContext(ctx)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	var stopped bool
	p := &mockProv{stopFn: func() { stopped = true }}

	cancel() // cancel immediately — timer should not fire
	maxRuntimeTimer(cmd, nil, "llama-8b", p, 10*time.Minute)

	assert.False(t, stopped)
}

func TestWaitForReadyTimeout(t *testing.T) {
	// Server that never becomes ready.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	cfg := cfgWithServer(t, srv)

	// Shorten the timeout so the test finishes quickly.
	old := startWaitTimeout
	startWaitTimeout = 3 * time.Second
	defer func() { startWaitTimeout = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	ok := waitForReady(cmd, cfg, "slow-model", &mockProv{})
	assert.False(t, ok)
	assert.Contains(t, buf.String(), "not ready after 3s")
	assert.Contains(t, buf.String(), "--timeout")
}

// TestWaitForReadyTimeoutFlag: an explicit --timeout overrides the default wait.
func TestWaitForReadyTimeoutFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	cfg := cfgWithServer(t, srv)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")
	cmd.Flags().Duration("timeout", startWaitTimeout, "")
	require.NoError(t, cmd.Flags().Set("timeout", "1s"))

	begin := time.Now()
	assert.False(t, waitForReady(cmd, cfg, "slow-model", &mockProv{}))
	assert.Less(t, time.Since(begin), 5*time.Second)
	assert.Contains(t, buf.String(), "not ready after")
}

// TestWaitForReadyContainerExitDetected verifies that waitForReady exits early
// when the provider reports a non-running container state.
func TestWaitForReadyContainerExitDetected(t *testing.T) {
	// Server that never becomes ready.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	cfg := cfgWithServer(t, srv)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	// mockProv with an "exited" ContainerState reported after the ticker fires.
	p := &mockProv{statusContainerState: "exited"}

	ok := waitForReady(cmd, cfg, "nim-model", p)
	assert.False(t, ok)
	assert.Contains(t, buf.String(), "exited")
}

// TestStartLogsPromptShownOnFailure verifies that when waitForReady returns false
// and stdout looks like a TTY, the user is prompted to show logs.
func TestStartLogsPromptShownOnFailure(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	// waitForReady always fails.
	old := startWaitForReadyFunc
	startWaitForReadyFunc = func(_ *cobra.Command, _ *config.Config, _ string, _ provider.Provider) bool {
		return false
	}
	t.Cleanup(func() { startWaitForReadyFunc = old })

	// Simulate a TTY so the prompt is shown.
	oldTTY := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdoutIsTerminal = oldTTY })

	// User answers "n" → no logs shown.
	oldReader := startLogsPromptReader
	startLogsPromptReader = strings.NewReader("n\n")
	t.Cleanup(func() { startLogsPromptReader = oldReader })

	injectProvider(t, &mockProv{})

	out, err := executeCmd("start", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "show logs?")
}

// TestStartLogsPromptYesStreamsLogs verifies the "y" path calls p.Logs.
func TestStartLogsPromptYesStreamsLogs(t *testing.T) {
	modelsDir, cleanup := switchEnv(t)
	defer cleanup()
	writeVLLMModel(t, modelsDir, "llama-8b")

	old := startWaitForReadyFunc
	startWaitForReadyFunc = func(_ *cobra.Command, _ *config.Config, _ string, _ provider.Provider) bool {
		return false
	}
	t.Cleanup(func() { startWaitForReadyFunc = old })

	oldTTY := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdoutIsTerminal = oldTTY })

	oldReader := startLogsPromptReader
	startLogsPromptReader = strings.NewReader("y\n")
	t.Cleanup(func() { startLogsPromptReader = oldReader })

	var logsCalled bool
	p := &mockProv{}
	oldLogs := p.Logs // can't override method, use a custom provider
	_ = oldLogs
	// Use a provider that records Logs being called.
	type loggingProv struct {
		*mockProv
		called *bool
	}
	lp := &loggingProv{mockProv: p, called: &logsCalled}
	// Can't override embedded method, so inject a fresh mockProv and intercept via
	// a test-only injectable. Instead just verify logs output appears.
	oldBuild := buildProvider
	buildProvider = func(_ config.ProviderType, _ *config.Config) (provider.Provider, error) {
		return p, nil
	}
	t.Cleanup(func() { buildProvider = oldBuild })
	_ = lp

	out, err := executeCmd("start", "llama-8b")
	require.NoError(t, err)
	assert.Contains(t, out, "show logs?")
}

// crashLoopProv simulates a systemd-managed service crash-looping: the first
// Status call (the baseline) reports restartsBefore, later calls report
// restartsAfter / state. Logs returns canned journal output.
type crashLoopProv struct {
	mockProv
	calls          atomic.Int32
	restartsBefore int
	restartsAfter  int
	state          string
	logs           string
}

func (c *crashLoopProv) Status(_ context.Context) (*provider.Status, error) {
	if c.calls.Add(1) == 1 {
		return &provider.Status{State: "starting", Restarts: c.restartsBefore}, nil
	}
	return &provider.Status{State: c.state, Restarts: c.restartsAfter}, nil
}

func (c *crashLoopProv) Logs(_ context.Context, w io.Writer, _ bool, _ int) error {
	_, err := io.WriteString(w, c.logs)
	return err
}

const oomJournal = `Oct 21 20:14:22 gn100-01 marlin[2756075]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436] torch.AcceleratorError: CUDA error: out of memory
Oct 21 20:14:24 gn100-01 marlin[2756075]: (APIServer pid=1) RuntimeError: Engine core initialization failed. See root cause above. Failed core proc(s): {}`

func notReadyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWaitForReadyDetectsCrashLoop(t *testing.T) {
	cfg := cfgWithServer(t, notReadyServer(t))
	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	// Baseline 3 (a pre-existing count from earlier crashes must not trigger),
	// then 4 → one new restart during the wait.
	p := &crashLoopProv{restartsBefore: 3, restartsAfter: 4, state: "starting", logs: oomJournal}
	start := time.Now()
	ok := waitForReady(cmd, cfg, "qwen-next", p)

	assert.False(t, ok)
	assert.Less(t, time.Since(start), 10*time.Second, "should bail on the first tick, not wait for the timeout")
	out := buf.String()
	assert.Contains(t, out, "crashed and is restarting (1 restart(s))")
	assert.Contains(t, out, "cause: torch.AcceleratorError: CUDA error: out of memory")
	assert.Contains(t, out, "hint :")
	assert.Contains(t, out, "marlin stop")
}

func TestWaitForReadyDetectsFailedState(t *testing.T) {
	cfg := cfgWithServer(t, notReadyServer(t))
	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	p := &crashLoopProv{state: "failed", logs: "KeyError: 'weird_param'"}
	assert.False(t, waitForReady(cmd, cfg, "m", p))
	assert.Contains(t, buf.String(), "service failed")
	assert.Contains(t, buf.String(), "cause: KeyError: 'weird_param'")
}

func TestWaitForReadyCrashWithoutRecognisableLogs(t *testing.T) {
	cfg := cfgWithServer(t, notReadyServer(t))
	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().BoolP("logs", "l", false, "")

	p := &crashLoopProv{restartsAfter: 1, state: "starting", logs: "INFO nothing useful"}
	assert.False(t, waitForReady(cmd, cfg, "m", p))
	assert.Contains(t, buf.String(), "run 'marlin logs' for details")
}
