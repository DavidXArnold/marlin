package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ExecRunner is the function signature used to run external commands.
// Exported so callers can inject a stub for testing.
type ExecRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type SystemdManager struct {
	unit       string
	execRunner ExecRunner
	sudo       bool // prefix systemctl with sudo when true
}

func NewSystemdManager(unit string) *SystemdManager {
	return &SystemdManager{
		unit:       unit,
		execRunner: defaultExecRunner,
		sudo:       os.Getuid() != 0,
	}
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

// Enable enables the unit to start automatically at boot.
func (s *SystemdManager) Enable(ctx context.Context) error {
	return s.systemctl(ctx, "enable")
}

// IsEnabled reports whether the unit is enabled at boot.
// Returns false (not an error) for disabled, static, or masked units.
func (s *SystemdManager) IsEnabled(ctx context.Context) (bool, error) {
	_, err := s.execRunner(ctx, "systemctl", "is-enabled", "--quiet", s.unit)
	if err != nil {
		if ec, ok := err.(exitCoder); ok && ec.ExitCode() > 0 {
			return false, nil
		}
		return false, fmt.Errorf("systemctl is-enabled %s: %w", s.unit, err)
	}
	return true, nil
}

func (s *SystemdManager) DaemonReload(ctx context.Context) error {
	var out []byte
	var err error
	if s.sudo {
		out, err = s.execRunner(ctx, "sudo", "systemctl", "daemon-reload")
	} else {
		out, err = s.execRunner(ctx, "systemctl", "daemon-reload")
	}
	if err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w\n%s", err, out)
	}
	return nil
}

func (s *SystemdManager) systemctl(ctx context.Context, action string) error {
	var out []byte
	var err error
	if s.sudo {
		out, err = s.execRunner(ctx, "sudo", "systemctl", action, s.unit)
	} else {
		out, err = s.execRunner(ctx, "systemctl", action, s.unit)
	}
	if err != nil {
		if ec, ok := err.(exitCoder); ok && ec.ExitCode() == 5 {
			return fmt.Errorf("systemd unit %q not found — create it with 'marlin install'", s.unit)
		}
		return fmt.Errorf("systemctl %s %s: %w\n%s", action, s.unit, err, out)
	}
	return nil
}
