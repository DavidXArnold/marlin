package privilege

import (
	"bytes"
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
	sudoTeeRun = func(path string, data []byte) error {
		cmd := exec.Command("sudo", "tee", path)
		cmd.Stdin = bytes.NewReader(data)
		cmd.Stdout = io.Discard
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

// WriteFileAsSudo creates dir via "sudo mkdir -p" then writes data to path via
// "sudo tee". The sudo password prompt (if any) is shown on the terminal.
func WriteFileAsSudo(dir, path string, data []byte) error {
	if err := sudoRun([]string{"mkdir", "-p", dir}); err != nil {
		return fmt.Errorf("sudo mkdir -p %s: %w", dir, err)
	}
	if err := sudoTeeRun(path, data); err != nil {
		return fmt.Errorf("sudo tee %s: %w", path, err)
	}
	return nil
}

// PromptAndWriteFile writes data to path in dir. If the dir is user-writable,
// it creates it with os.MkdirAll and writes directly. If root is required, it
// prints a warning to w, reads y/yes confirmation from stdin, then writes with
// sudo. Returns (false, nil) if the user declines (prints "cancelled" itself).
func PromptAndWriteFile(w io.Writer, dir, path string, data []byte) (bool, error) {
	if !NeedsRoot(dir) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return false, err
		}
		return true, nil
	}
	_, _ = fmt.Fprintf(w, "\nwarning: writing to %s requires administrator privileges\n", dir)
	_, _ = fmt.Fprint(w, "continue with sudo? [y/N] ")
	buf := make([]byte, 64)
	n, _ := stdinR.Read(buf)
	answer := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	if answer != "y" && answer != "yes" {
		_, _ = fmt.Fprintln(w, "cancelled")
		return false, nil
	}
	if err := WriteFileAsSudo(dir, path, data); err != nil {
		return false, err
	}
	return true, nil
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
