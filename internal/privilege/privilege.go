package privilege

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// PromptAndMkdirAll creates dir and all parents. If root is required, warns on w,
// prompts for y/n confirmation, then creates via sudo mkdir -p.
func PromptAndMkdirAll(w io.Writer, dir string) error {
	if !NeedsRoot(dir) {
		return os.MkdirAll(dir, 0o755)
	}
	_, _ = fmt.Fprintf(w, "\nwarning: creating %s requires administrator privileges\n", dir)
	_, _ = fmt.Fprint(w, "continue with sudo? [y/N] ")
	buf := make([]byte, 64)
	n, _ := stdinR.Read(buf)
	if strings.ToLower(strings.TrimSpace(string(buf[:n]))) != "y" {
		_, _ = fmt.Fprintln(w, "cancelled")
		return fmt.Errorf("cancelled")
	}
	return sudoRun([]string{"mkdir", "-p", dir})
}

// PromptAndPrepareNIMCache creates dir and sets GID-0 group write permissions
// required by NIM containers (which run as UID=1000, GID=0). If the directory
// already exists with the correct group and permissions this is a no-op.
// Otherwise it warns on w and prompts once before running all three steps via sudo.
func PromptAndPrepareNIMCache(w io.Writer, dir string) error {
	if nimCacheReady(dir) {
		return nil
	}
	needsRoot := NeedsRoot(dir)
	_, _ = fmt.Fprintf(w, "\nwarning: NIM cache setup for %s requires administrator privileges\n", dir)
	_, _ = fmt.Fprintf(w, "  will run: mkdir -p, chgrp -R 0, chmod -R g+rwX\n")
	_, _ = fmt.Fprint(w, "continue with sudo? [y/N] ")
	buf := make([]byte, 64)
	n, _ := stdinR.Read(buf)
	if strings.ToLower(strings.TrimSpace(string(buf[:n]))) != "y" {
		_, _ = fmt.Fprintln(w, "cancelled")
		return fmt.Errorf("cancelled")
	}
	if needsRoot {
		if err := sudoRun([]string{"mkdir", "-p", dir}); err != nil {
			return err
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := sudoRun([]string{"chgrp", "-R", "0", dir}); err != nil {
		return err
	}
	return sudoRun([]string{"chmod", "-R", "g+rwX", dir})
}

// nimCacheReady reports whether dir exists, is owned by GID 0, and has group rwx.
func nimCacheReady(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return st.Gid == 0 && info.Mode()&0o070 == 0o070
}

// RefreshNIMCachePerms re-applies group write permissions on dir so that
// subdirectories created by a previous NIM container run remain accessible to
// the container user (UID=1000, GID=0). Runs via sudo using the system
// credential cache — no custom [y/N] prompt is shown.
func RefreshNIMCachePerms(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return nil // dir doesn't exist yet; PromptAndPrepareNIMCache will handle it
	}
	return sudoRun([]string{"chmod", "-R", "g+rwX", dir})
}

// PromptAndRemove removes path. If the removal fails with a permission error,
// it warns on w, prompts for y/n confirmation, then retries via sudo rm.
func PromptAndRemove(w io.Writer, path string) error {
	if err := os.Remove(path); err == nil {
		return nil
	} else if !os.IsPermission(err) {
		return err
	}
	_, _ = fmt.Fprintf(w, "\nwarning: removing %s requires administrator privileges\n", path)
	_, _ = fmt.Fprint(w, "continue with sudo? [y/N] ")
	buf := make([]byte, 64)
	n, _ := stdinR.Read(buf)
	if strings.ToLower(strings.TrimSpace(string(buf[:n]))) != "y" {
		_, _ = fmt.Fprintln(w, "cancelled")
		return fmt.Errorf("cancelled")
	}
	return sudoRun([]string{"rm", path})
}

// PromptAndSymlink creates or atomically replaces dst as a symlink pointing to src.
// If the target directory requires root, warns on w, prompts for confirmation, then
// falls back to sudo ln -sf. Returns a non-nil error (including "cancelled") on failure.
func PromptAndSymlink(w io.Writer, src, dst string) error {
	err := atomicSymlinkImpl(src, dst)
	if err == nil {
		return nil
	}
	if !os.IsPermission(err) {
		return err
	}
	_, _ = fmt.Fprintf(w, "\nwarning: updating symlink at %s requires administrator privileges\n", dst)
	_, _ = fmt.Fprint(w, "continue with sudo? [y/N] ")
	buf := make([]byte, 64)
	n, _ := stdinR.Read(buf)
	if strings.ToLower(strings.TrimSpace(string(buf[:n]))) != "y" {
		_, _ = fmt.Fprintln(w, "cancelled")
		return fmt.Errorf("cancelled")
	}
	return sudoRun([]string{"ln", "-sf", src, dst})
}

// atomicSymlinkImpl replaces dst so it points at src, atomically via a
// temp symlink + rename so the link is never absent during the swap.
func atomicSymlinkImpl(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(src, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
