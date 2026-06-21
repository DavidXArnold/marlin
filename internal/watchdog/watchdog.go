package watchdog

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Config controls watchdog polling and restart behaviour.
type Config struct {
	Interval      time.Duration // how often to check the health endpoint
	MaxRestarts   int           // max restarts in RestartWindow before giving up; 0 = unlimited
	RestartWindow time.Duration // rolling window for counting restarts
	RestartDelay  time.Duration // pause between failure detection and restart attempt
}

// nowFunc and afterFunc are injectable for tests.
var nowFunc = time.Now
var afterFunc = func(d time.Duration) <-chan time.Time { return time.After(d) }

// SetAfterFuncForTest replaces the timer function and returns a restore func.
func SetAfterFuncForTest(fn func(time.Duration) <-chan time.Time) func() {
	old := afterFunc
	afterFunc = fn
	return func() { afterFunc = old }
}

// SetNowFuncForTest replaces time.Now and returns a restore func.
func SetNowFuncForTest(fn func() time.Time) func() {
	old := nowFunc
	nowFunc = fn
	return func() { nowFunc = old }
}

// Run polls isHealthy at cfg.Interval and calls restart when it returns false.
// It returns nil when ctx is cancelled, or an error when MaxRestarts is exceeded
// or restart itself fails.
func Run(
	ctx context.Context,
	cfg Config,
	isHealthy func(context.Context) bool,
	slug string,
	restart func(context.Context) error,
	w io.Writer,
) error {
	restartTimes := make([]time.Time, 0, max(cfg.MaxRestarts, 8))

	_, _ = fmt.Fprintf(w, "[watchdog] watching %s every %s", slug, cfg.Interval)
	if cfg.MaxRestarts > 0 {
		_, _ = fmt.Fprintf(w, " (max %d restarts per %s)", cfg.MaxRestarts, cfg.RestartWindow)
	}
	_, _ = fmt.Fprintln(w)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-afterFunc(cfg.Interval):
		}

		if isHealthy(ctx) {
			continue
		}

		_, _ = fmt.Fprintf(w, "[watchdog] %s health check failed — checking restart policy\n", slug)

		// Prune restart timestamps outside the rolling window.
		if cfg.RestartWindow > 0 {
			cutoff := nowFunc().Add(-cfg.RestartWindow)
			j := 0
			for _, t := range restartTimes {
				if t.After(cutoff) {
					restartTimes[j] = t
					j++
				}
			}
			restartTimes = restartTimes[:j]
		}

		if cfg.MaxRestarts > 0 && len(restartTimes) >= cfg.MaxRestarts {
			return fmt.Errorf("watchdog: max restarts (%d in %s) exceeded — giving up", cfg.MaxRestarts, cfg.RestartWindow)
		}

		// Brief delay so a momentary hiccup doesn't trigger an unnecessary restart.
		if cfg.RestartDelay > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-afterFunc(cfg.RestartDelay):
			}
		}

		_, _ = fmt.Fprintf(w, "[watchdog] restarting %s\n", slug)
		if err := restart(ctx); err != nil {
			return fmt.Errorf("watchdog: restart failed: %w", err)
		}
		restartTimes = append(restartTimes, nowFunc())
		_, _ = fmt.Fprintf(w, "[watchdog] %s restarted (restarts in window: %d)\n", slug, len(restartTimes))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
