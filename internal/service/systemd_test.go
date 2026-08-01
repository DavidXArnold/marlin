package service

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func successRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte(""), nil
}

func failRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("unit not found"), fmt.Errorf("exit status 1")
}

// inactiveErr simulates systemctl exit code 3 (unit inactive).
type inactiveErr struct{}

func (e *inactiveErr) Error() string { return "exit status 3" }
func (e *inactiveErr) ExitCode() int { return 3 }

func inactiveRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("inactive"), &inactiveErr{}
}

func TestNewSystemdManager(t *testing.T) {
	m := NewSystemdManager("vllm.service")
	assert.Equal(t, "vllm.service", m.unit)
	assert.NotNil(t, m.execRunner)
}

func TestDefaultExecRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command path differs on Windows")
	}
	out, err := defaultExecRunner(context.Background(), "printf", "ok")
	require.NoError(t, err)
	assert.Equal(t, "ok", string(out))
}

func TestNewSystemdManagerWithRunner(t *testing.T) {
	m := NewSystemdManagerWithRunner("vllm.service", successRunner)
	assert.Equal(t, "vllm.service", m.unit)
	require.NoError(t, m.Restart(context.Background()))
}

func TestRestart(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	require.NoError(t, m.Restart(context.Background()))
}

func TestRestartFail(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	err := m.Restart(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "restart")
}

func TestStop(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	require.NoError(t, m.Stop(context.Background()))
}

func TestStopFail(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	assert.Error(t, m.Stop(context.Background()))
}

func TestStart(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	require.NoError(t, m.Start(context.Background()))
}

func TestStartFail(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	assert.Error(t, m.Start(context.Background()))
}

func TestIsActive_Active(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	active, err := m.IsActive(context.Background())
	require.NoError(t, err)
	assert.True(t, active)
}

func TestIsActive_Inactive(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: inactiveRunner}
	active, err := m.IsActive(context.Background())
	require.NoError(t, err)
	assert.False(t, active)
}

func TestIsActive_Error(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	_, err := m.IsActive(context.Background())
	assert.Error(t, err)
}

func TestActiveState_Active(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	state, err := m.ActiveState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "active", state)
}

func TestActiveState_Inactive(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: inactiveRunner}
	state, err := m.ActiveState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "inactive", state)
}

func TestActiveState_Activating(t *testing.T) {
	activatingRunner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("activating"), &inactiveErr{}
	}
	m := &SystemdManager{unit: "vllm.service", execRunner: activatingRunner}
	state, err := m.ActiveState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "activating", state)
}

func TestActiveState_Deactivating(t *testing.T) {
	deactivatingRunner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("deactivating"), &inactiveErr{}
	}
	m := &SystemdManager{unit: "vllm.service", execRunner: deactivatingRunner}
	state, err := m.ActiveState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "deactivating", state)
}

func TestActiveState_Error(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	_, err := m.ActiveState(context.Background())
	assert.Error(t, err)
}

func TestFriendlyState(t *testing.T) {
	cases := map[string]string{
		"active":       "running",
		"reloading":    "running",
		"activating":   "starting",
		"deactivating": "stopping",
		"inactive":     "stopped",
		"failed":       "failed",
		"":             "unknown",
		"bogus":        "unknown",
	}
	for raw, want := range cases {
		assert.Equal(t, want, FriendlyState(raw), "raw=%q", raw)
	}
}

func TestEnable(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	require.NoError(t, m.Enable(context.Background()))
}

func TestEnableFail(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	assert.Error(t, m.Enable(context.Background()))
}

func TestIsEnabled_Enabled(t *testing.T) {
	m := &SystemdManager{unit: "vllm.service", execRunner: successRunner}
	enabled, err := m.IsEnabled(context.Background())
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsEnabled_Disabled(t *testing.T) {
	// exit code 1 → disabled, not an error
	m := &SystemdManager{unit: "vllm.service", execRunner: inactiveRunner}
	enabled, err := m.IsEnabled(context.Background())
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestIsEnabled_Error(t *testing.T) {
	// failRunner returns exit code 1 via a plain error — treated as disabled
	m := &SystemdManager{unit: "vllm.service", execRunner: failRunner}
	// failRunner's error doesn't implement exitCoder so it's a real error
	_, err := m.IsEnabled(context.Background())
	assert.Error(t, err)
}

// notFoundErr simulates systemctl exit code 5 (unit not found).
type notFoundErr struct{}

func (e *notFoundErr) Error() string { return "exit status 5" }
func (e *notFoundErr) ExitCode() int { return 5 }

func notFoundRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("Failed to restart marlin.service: Unit marlin.service not found."), &notFoundErr{}
}

func TestRestartUnitNotFoundSuggestsInstall(t *testing.T) {
	m := &SystemdManager{unit: "marlin", execRunner: notFoundRunner}
	err := m.Restart(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "marlin install")
}

func TestStopUnitNotFoundIsNoOp(t *testing.T) {
	// Stopping a unit that doesn't exist (exit 5) should succeed — it's already not running.
	m := &SystemdManager{unit: "marlin", execRunner: notFoundRunner}
	require.NoError(t, m.Stop(context.Background()))
}

func TestDaemonReload(t *testing.T) {
	m := NewSystemdManagerWithRunner("marlin", successRunner)
	require.NoError(t, m.DaemonReload(context.Background()))
}

func TestDaemonReloadFail(t *testing.T) {
	m := NewSystemdManagerWithRunner("marlin", failRunner)
	err := m.DaemonReload(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "daemon-reload")
}
