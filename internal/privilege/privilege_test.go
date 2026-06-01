package privilege

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequireRootWhenRoot(t *testing.T) {
	old := getuid
	getuid = func() int { return 0 }
	defer func() { getuid = old }()

	RequireRoot()
}

func TestRequireRootSudoSucceeds(t *testing.T) {
	oldGetuid := getuid
	oldExit := osExit
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	var exitCode int
	osExit = func(code int) { exitCode = code }
	sudoRun = func(_ []string) error { return nil }
	defer func() {
		getuid = oldGetuid
		osExit = oldExit
		sudoRun = oldSudo
	}()

	RequireRoot()
	assert.Equal(t, 0, exitCode)
}

func TestRequireRootSudoFails(t *testing.T) {
	oldGetuid := getuid
	oldExit := osExit
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	var exitCode int
	osExit = func(code int) { exitCode = code }
	sudoRun = func(_ []string) error { return fmt.Errorf("sudo: command not found") }
	defer func() {
		getuid = oldGetuid
		osExit = oldExit
		sudoRun = oldSudo
	}()

	RequireRoot()
	assert.Equal(t, 1, exitCode)
}

// — NeedsRoot —

func TestNeedsRootWhenAlreadyRoot(t *testing.T) {
	old := getuid
	getuid = func() int { return 0 }
	defer func() { getuid = old }()

	assert.False(t, NeedsRoot("/etc/marlin"))
}

func TestNeedsRootWritableDir(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	assert.False(t, NeedsRoot(t.TempDir()))
}

func TestNeedsRootNonWritableDir(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot change dir permissions")
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()

	assert.True(t, NeedsRoot(dir))
}

func TestNeedsRootNonExistentUnderWritable(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	assert.False(t, NeedsRoot(t.TempDir()+"/does/not/exist"))
}

// nonWritableDir creates a temp dir with 0555 permissions (no write).
// It restores permissions on cleanup so the test runner can delete it.
func nonWritableDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot change dir permissions")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return dir
}

// — WarnAndRequireRoot —

func TestWarnAndRequireRootWritablePath(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	// writable temp dir → no warning, no prompt
	var buf bytes.Buffer
	WarnAndRequireRoot(&buf, t.TempDir())
	assert.Empty(t, buf.String())
}

func TestWarnAndRequireRootAlreadyRoot(t *testing.T) {
	old := getuid
	getuid = func() int { return 0 }
	defer func() { getuid = old }()

	var buf bytes.Buffer
	WarnAndRequireRoot(&buf, nonWritableDir(t))
	assert.Empty(t, buf.String())
}

func TestWarnAndRequireRootUserConfirms(t *testing.T) {
	oldGetuid := getuid
	oldExit := osExit
	oldSudo := sudoRun
	oldStdin := stdinR
	getuid = func() int { return 1000 }
	var exitCode int
	osExit = func(code int) { exitCode = code }
	sudoRun = func(_ []string) error { return nil }
	stdinR = strings.NewReader("y\n")
	defer func() {
		getuid = oldGetuid
		osExit = oldExit
		sudoRun = oldSudo
		stdinR = oldStdin
	}()

	dir := nonWritableDir(t)
	var buf bytes.Buffer
	WarnAndRequireRoot(&buf, dir)
	assert.Contains(t, buf.String(), "warning:")
	assert.Contains(t, buf.String(), dir)
	assert.Equal(t, 0, exitCode)
}

func TestWarnAndRequireRootUserDeclines(t *testing.T) {
	oldGetuid := getuid
	oldExit := osExit
	oldSudo := sudoRun
	oldStdin := stdinR
	getuid = func() int { return 1000 }
	var exitCode int
	osExit = func(code int) { exitCode = code }
	sudoRun = func(_ []string) error { t.Fatal("sudo should not be called"); return nil }
	stdinR = strings.NewReader("n\n")
	defer func() {
		getuid = oldGetuid
		osExit = oldExit
		sudoRun = oldSudo
		stdinR = oldStdin
	}()

	var buf bytes.Buffer
	WarnAndRequireRoot(&buf, nonWritableDir(t))
	assert.Contains(t, buf.String(), "cancelled")
	assert.Equal(t, 0, exitCode)
}

func TestWarnAndRequireRootSudoFails(t *testing.T) {
	oldGetuid := getuid
	oldExit := osExit
	oldSudo := sudoRun
	oldStdin := stdinR
	getuid = func() int { return 1000 }
	var exitCode int
	osExit = func(code int) { exitCode = code }
	sudoRun = func(_ []string) error { return fmt.Errorf("sudo failed") }
	stdinR = strings.NewReader("yes\n")
	defer func() {
		getuid = oldGetuid
		osExit = oldExit
		sudoRun = oldSudo
		stdinR = oldStdin
	}()

	var buf bytes.Buffer
	WarnAndRequireRoot(&buf, nonWritableDir(t))
	assert.Equal(t, 1, exitCode)
}
