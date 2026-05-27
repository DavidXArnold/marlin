package service

import (
	"context"
	"fmt"
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

func (e *inactiveErr) Error() string  { return "exit status 3" }
func (e *inactiveErr) ExitCode() int  { return 3 }

func inactiveRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("inactive"), &inactiveErr{}
}

func TestNewSystemdManager(t *testing.T) {
	m := NewSystemdManager("vllm.service")
	assert.Equal(t, "vllm.service", m.unit)
	assert.NotNil(t, m.execRunner)
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
