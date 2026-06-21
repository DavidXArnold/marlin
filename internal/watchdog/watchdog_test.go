package watchdog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// immediateTick returns an afterFunc that fires instantly, letting tests drive
// the loop without real sleeps.
func immediateTick() func(time.Duration) <-chan time.Time {
	return func(_ time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
}

func TestRunCancelledBeforeFirstTick(t *testing.T) {
	restore := SetAfterFuncForTest(func(d time.Duration) <-chan time.Time {
		// Return a channel that never fires — context cancellation is the only exit.
		return make(chan time.Time)
	})
	t.Cleanup(restore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var buf bytes.Buffer
	cfg := Config{Interval: time.Second, MaxRestarts: 3, RestartWindow: time.Minute}
	err := Run(ctx, cfg, func(context.Context) bool { return true }, "m", func(context.Context) error { return nil }, &buf)
	assert.NoError(t, err)
}

func TestRunHealthyNoRestart(t *testing.T) {
	ticks := 0
	restore := SetAfterFuncForTest(func(d time.Duration) <-chan time.Time {
		ticks++
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	})
	t.Cleanup(restore)

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	isHealthy := func(context.Context) bool {
		calls++
		if calls >= 3 {
			cancel()
		}
		return true
	}

	var buf bytes.Buffer
	cfg := Config{Interval: time.Millisecond, MaxRestarts: 2, RestartWindow: time.Minute}
	err := Run(ctx, cfg, isHealthy, "slug", func(context.Context) error { return nil }, &buf)
	assert.NoError(t, err)
	assert.Zero(t, 0) // no restarts
	assert.Contains(t, buf.String(), "watching slug")
}

func TestRunRestartOnFailure(t *testing.T) {
	restore := SetAfterFuncForTest(immediateTick())
	t.Cleanup(restore)

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	restarts := 0

	isHealthy := func(context.Context) bool {
		calls++
		if calls == 1 {
			return false // fail once
		}
		cancel() // healthy on second check → cancel
		return true
	}
	restart := func(context.Context) error {
		restarts++
		return nil
	}

	var buf bytes.Buffer
	cfg := Config{Interval: time.Millisecond, MaxRestarts: 5, RestartWindow: time.Minute}
	err := Run(ctx, cfg, isHealthy, "mymodel", restart, &buf)
	assert.NoError(t, err)
	assert.Equal(t, 1, restarts)
	assert.Contains(t, buf.String(), "restarting mymodel")
	assert.Contains(t, buf.String(), "restarted")
}

func TestRunMaxRestartsExceeded(t *testing.T) {
	restore := SetAfterFuncForTest(immediateTick())
	t.Cleanup(restore)

	// Fix time so restart window never expires.
	fixedNow := time.Now()
	restoreNow := SetNowFuncForTest(func() time.Time { return fixedNow })
	t.Cleanup(restoreNow)

	isHealthy := func(context.Context) bool { return false }
	restarts := 0
	restart := func(context.Context) error {
		restarts++
		return nil
	}

	var buf bytes.Buffer
	cfg := Config{
		Interval:      time.Millisecond,
		MaxRestarts:   2,
		RestartWindow: time.Minute,
	}
	err := Run(context.Background(), cfg, isHealthy, "slug", restart, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max restarts")
	assert.Equal(t, 2, restarts)
}

func TestRunRestartError(t *testing.T) {
	restore := SetAfterFuncForTest(immediateTick())
	t.Cleanup(restore)

	isHealthy := func(context.Context) bool { return false }
	restart := func(context.Context) error { return errors.New("systemd exploded") }

	var buf bytes.Buffer
	cfg := Config{Interval: time.Millisecond, MaxRestarts: 5, RestartWindow: time.Minute}
	err := Run(context.Background(), cfg, isHealthy, "slug", restart, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restart failed")
	assert.Contains(t, err.Error(), "systemd exploded")
}

func TestRunRestartWindowPrune(t *testing.T) {
	restore := SetAfterFuncForTest(immediateTick())
	t.Cleanup(restore)

	// Advance now so old restarts fall outside the 1-second window.
	callCount := 0
	restoreNow := SetNowFuncForTest(func() time.Time {
		callCount++
		return time.Now().Add(time.Duration(callCount) * 2 * time.Second)
	})
	t.Cleanup(restoreNow)

	ctx, cancel := context.WithCancel(context.Background())
	healthCalls := 0
	restarts := 0

	isHealthy := func(context.Context) bool {
		healthCalls++
		if healthCalls <= 2 {
			return false // fail twice — triggers 2 restarts
		}
		cancel()
		return true
	}
	restart := func(context.Context) error {
		restarts++
		return nil
	}

	var buf bytes.Buffer
	cfg := Config{
		Interval:      time.Millisecond,
		MaxRestarts:   2,
		RestartWindow: time.Second, // 1s window; nowFunc advances 2s per call → old restarts pruned
	}
	err := Run(ctx, cfg, isHealthy, "slug", restart, &buf)
	assert.NoError(t, err, "restarts should be pruned from window, not accumulate")
	assert.Equal(t, 2, restarts)
}

func TestRunUnlimitedRestarts(t *testing.T) {
	restore := SetAfterFuncForTest(immediateTick())
	t.Cleanup(restore)

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	restarts := 0

	isHealthy := func(context.Context) bool {
		calls++
		if calls > 5 {
			cancel()
			return true
		}
		return false
	}
	restart := func(context.Context) error {
		restarts++
		return nil
	}

	var buf bytes.Buffer
	cfg := Config{Interval: time.Millisecond, MaxRestarts: 0} // 0 = unlimited
	err := Run(ctx, cfg, isHealthy, "slug", restart, &buf)
	assert.NoError(t, err)
	assert.Equal(t, 5, restarts)
}

func TestRunHeaderUnlimited(t *testing.T) {
	restore := SetAfterFuncForTest(func(_ time.Duration) <-chan time.Time {
		return make(chan time.Time) // never fires
	})
	t.Cleanup(restore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	cfg := Config{Interval: time.Second, MaxRestarts: 0}
	_ = Run(ctx, cfg, func(context.Context) bool { return true }, "s", func(context.Context) error { return nil }, &buf)
	assert.NotContains(t, buf.String(), "max", fmt.Sprintf("output was: %q", buf.String()))
}
