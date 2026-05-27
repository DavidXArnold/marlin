package service

import (
	"context"
	"fmt"
	"os/exec"
)

// ExecRunner is the function signature used to run external commands.
// Exported so callers can inject a stub for testing.
type ExecRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type SystemdManager struct {
	unit       string
	execRunner ExecRunner
}

func NewSystemdManager(unit string) *SystemdManager {
	return &SystemdManager{unit: unit, execRunner: defaultExecRunner}
}

// NewSystemdManagerWithRunner creates a SystemdManager with a custom runner,
// used in tests to avoid invoking real systemctl.
func NewSystemdManagerWithRunner(unit string, r ExecRunner) *SystemdManager {
	return &SystemdManager{unit: unit, execRunner: r}
}

func defaultExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s *SystemdManager) Restart(ctx context.Context) error {
	return s.systemctl(ctx, "restart")
}

func (s *SystemdManager) Stop(ctx context.Context) error {
	return s.systemctl(ctx, "stop")
}

func (s *SystemdManager) Start(ctx context.Context) error {
	return s.systemctl(ctx, "start")
}

type exitCoder interface {
	ExitCode() int
}

func (s *SystemdManager) IsActive(ctx context.Context) (bool, error) {
	out, err := s.execRunner(ctx, "systemctl", "is-active", "--quiet", s.unit)
	if err != nil {
		if ec, ok := err.(exitCoder); ok && ec.ExitCode() == 3 {
			return false, nil
		}
		return false, fmt.Errorf("systemctl is-active %s: %w\n%s", s.unit, err, out)
	}
	return true, nil
}

func (s *SystemdManager) systemctl(ctx context.Context, action string) error {
	out, err := s.execRunner(ctx, "systemctl", action, s.unit)
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w\n%s", action, s.unit, err, out)
	}
	return nil
}
