package provider

import (
	"context"
	"io"
	"os/exec"
)

// runCommand executes name with args, streaming combined output to w.
// It is used for log tailing (journalctl) and docker logs.
var runCommand = func(ctx context.Context, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// SetRunCommandForTest replaces runCommand with fn and returns a restore func.
// Only for use in tests.
func SetRunCommandForTest(fn func(context.Context, io.Writer, string, ...string) error) func() {
	old := runCommand
	runCommand = fn
	return func() { runCommand = old }
}
