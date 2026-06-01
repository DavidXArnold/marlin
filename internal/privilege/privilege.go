package privilege

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// injectable for tests
var (
	getuid  = os.Getuid
	osExit  = os.Exit
	stdinR  io.Reader = os.Stdin
	sudoRun           = func(args []string) error {
		cmd := exec.Command("sudo", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
)

// RequireRoot re-executes the current binary under sudo if not already running
// as root, transparently inheriting stdin/stdout/stderr so the sudo password
// prompt behaves exactly like systemctl's privilege escalation.
func RequireRoot() {
	if getuid() == 0 {
		return
	}
	if err := sudoRun(append([]string{os.Args[0]}, os.Args[1:]...)); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		osExit(1)
		return
	}
	osExit(0)
}

// NeedsRoot reports whether writing to dir (or its nearest existing ancestor)
// requires root privileges. Returns false when already root.
func NeedsRoot(dir string) bool {
	if getuid() == 0 {
		return false
	}
	// Walk up to the first existing ancestor directory.
	check := dir
	for {
		info, err := os.Stat(check)
		if err == nil {
			if !info.IsDir() {
				check = filepath.Dir(check)
				continue
			}
			f, err := os.CreateTemp(check, ".marlin-access-*")
			if err != nil {
				return true
			}
			_ = f.Close()
			_ = os.Remove(f.Name())
			return false
		}
		parent := filepath.Dir(check)
		if parent == check {
			return true
		}
		check = parent
	}
}

// WarnAndRequireRoot checks whether targetPath needs root access. If it does,
// it prints a warning, prompts for confirmation on stdin, then re-execs the
// current process under sudo. If the user declines, it prints "cancelled" and
// exits 0. If already root or the path is user-writable, it returns immediately.
func WarnAndRequireRoot(w io.Writer, targetPath string) {
	if !NeedsRoot(targetPath) {
		return
	}
	_, _ = fmt.Fprintf(w, "\nwarning: writing to %s requires administrator privileges\n", targetPath)
	_, _ = fmt.Fprint(w, "continue with sudo? [y/N] ")

	buf := make([]byte, 64)
	n, _ := stdinR.Read(buf)
	answer := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	if answer != "y" && answer != "yes" {
		_, _ = fmt.Fprintln(w, "cancelled")
		osExit(0)
		return
	}
	if err := sudoRun(append([]string{os.Args[0]}, os.Args[1:]...)); err != nil {
		_, _ = fmt.Fprintln(w, err)
		osExit(1)
		return
	}
	osExit(0)
}
