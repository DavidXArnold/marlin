package privilege

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// — WriteFileAsSudo —

func TestWriteFileAsSudoSuccess(t *testing.T) {
	oldSudo := sudoRun
	oldTee := sudoTeeRun
	sudoRun = func(_ []string) error { return nil }
	sudoTeeRun = func(_ string, _ []byte) error { return nil }
	defer func() { sudoRun = oldSudo; sudoTeeRun = oldTee }()

	assert.NoError(t, WriteFileAsSudo("/some/dir", "/some/dir/f.toml", []byte("data")))
}

func TestWriteFileAsSudoMkdirFails(t *testing.T) {
	oldSudo := sudoRun
	sudoRun = func(_ []string) error { return fmt.Errorf("permission denied") }
	defer func() { sudoRun = oldSudo }()

	err := WriteFileAsSudo("/some/dir", "/some/dir/f.toml", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sudo mkdir")
}

func TestWriteFileAsSudoTeeFails(t *testing.T) {
	oldSudo := sudoRun
	oldTee := sudoTeeRun
	sudoRun = func(_ []string) error { return nil }
	sudoTeeRun = func(_ string, _ []byte) error { return fmt.Errorf("tee failed") }
	defer func() { sudoRun = oldSudo; sudoTeeRun = oldTee }()

	err := WriteFileAsSudo("/some/dir", "/some/dir/f.toml", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sudo tee")
}

// — PromptAndWriteFile —

func TestPromptAndWriteFileWritable(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "model.toml")
	var buf bytes.Buffer
	written, err := PromptAndWriteFile(&buf, dir, path, []byte("[model]"))
	require.NoError(t, err)
	assert.True(t, written)
	assert.Empty(t, buf.String())
	data, _ := os.ReadFile(path)
	assert.Equal(t, "[model]", string(data))
}

func TestPromptAndWriteFileMkdirAllCreatesDir(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	base := t.TempDir()
	dir := filepath.Join(base, "sub", "dir")
	path := filepath.Join(dir, "model.toml")
	var buf bytes.Buffer
	written, err := PromptAndWriteFile(&buf, dir, path, []byte("[model]"))
	require.NoError(t, err)
	assert.True(t, written)
	data, _ := os.ReadFile(path)
	assert.Equal(t, "[model]", string(data))
}

func TestPromptAndWriteFileUserConfirms(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	oldTee := sudoTeeRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("y\n")
	sudoRun = func(_ []string) error { return nil }
	var written []byte
	sudoTeeRun = func(_ string, data []byte) error { written = data; return nil }
	defer func() {
		getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo; sudoTeeRun = oldTee
	}()

	dir := nonWritableDir(t)
	path := filepath.Join(dir, "f.toml")
	var buf bytes.Buffer
	ok, err := PromptAndWriteFile(&buf, dir, path, []byte("content"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []byte("content"), written)
	assert.Contains(t, buf.String(), "warning:")
}

func TestPromptAndWriteFileUserDeclines(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("n\n")
	sudoRun = func(_ []string) error { t.Fatal("sudo should not be called"); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	dir := nonWritableDir(t)
	var buf bytes.Buffer
	ok, err := PromptAndWriteFile(&buf, dir, filepath.Join(dir, "f.toml"), []byte("x"))
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Contains(t, buf.String(), "cancelled")
}

func TestPromptAndWriteFileSudoFails(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	oldTee := sudoTeeRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("yes\n")
	sudoRun = func(_ []string) error { return nil }
	sudoTeeRun = func(_ string, _ []byte) error { return fmt.Errorf("tee denied") }
	defer func() {
		getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo; sudoTeeRun = oldTee
	}()

	dir := nonWritableDir(t)
	var buf bytes.Buffer
	ok, err := PromptAndWriteFile(&buf, dir, filepath.Join(dir, "f.toml"), []byte("x"))
	assert.False(t, ok)
	assert.Error(t, err)
}
