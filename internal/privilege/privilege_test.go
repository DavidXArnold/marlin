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

// --- PromptAndRemove ---

func TestPromptAndRemoveSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.toml")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0644))

	var buf bytes.Buffer
	require.NoError(t, PromptAndRemove(&buf, path))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestPromptAndRemoveNotPermission(t *testing.T) {
	err := PromptAndRemove(os.Stderr, "/nonexistent/model.toml")
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "cancelled")
}

func TestPromptAndRemoveUserConfirms(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("y\n")
	var removedPath string
	sudoRun = func(args []string) error { removedPath = args[1]; return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	// Create the file before making the dir non-writable.
	base := t.TempDir()
	path := filepath.Join(base, "model.toml")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0644))
	require.NoError(t, os.Chmod(base, 0o555))
	t.Cleanup(func() { _ = os.Chmod(base, 0o755) })

	var buf bytes.Buffer
	err := PromptAndRemove(&buf, path)
	require.NoError(t, err)
	assert.Equal(t, path, removedPath)
	assert.Contains(t, buf.String(), "warning:")
}

func TestPromptAndRemoveUserDeclines(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("n\n")
	sudoRun = func(_ []string) error { t.Fatal("sudo should not be called"); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	base := t.TempDir()
	path := filepath.Join(base, "model.toml")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0644))
	require.NoError(t, os.Chmod(base, 0o555))
	t.Cleanup(func() { _ = os.Chmod(base, 0o755) })

	var buf bytes.Buffer
	err := PromptAndRemove(&buf, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// --- PromptAndSymlink + atomicSymlinkImpl ---

func TestAtomicSymlinkImplSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.env")
	dst := filepath.Join(dir, "link.env")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0644))

	require.NoError(t, atomicSymlinkImpl(src, dst))
	got, err := os.Readlink(dst)
	require.NoError(t, err)
	assert.Equal(t, src, got)
}

func TestAtomicSymlinkImplReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	srcA := filepath.Join(dir, "a.env")
	srcB := filepath.Join(dir, "b.env")
	dst := filepath.Join(dir, "link.env")
	require.NoError(t, os.WriteFile(srcA, []byte("a"), 0644))
	require.NoError(t, os.WriteFile(srcB, []byte("b"), 0644))

	require.NoError(t, atomicSymlinkImpl(srcA, dst))
	require.NoError(t, atomicSymlinkImpl(srcB, dst))
	got, err := os.Readlink(dst)
	require.NoError(t, err)
	assert.Equal(t, srcB, got)
}

func TestPromptAndSymlinkSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "model.env")
	dst := filepath.Join(dir, "active.env")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0644))

	var buf bytes.Buffer
	require.NoError(t, PromptAndSymlink(&buf, src, dst))
	got, err := os.Readlink(dst)
	require.NoError(t, err)
	assert.Equal(t, src, got)
}

func TestPromptAndSymlinkUserConfirms(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("y\n")
	var lnArgs []string
	sudoRun = func(args []string) error { lnArgs = args; return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	dir := nonWritableDir(t)
	src := filepath.Join(t.TempDir(), "model.env")
	dst := filepath.Join(dir, "active.env")
	var buf bytes.Buffer
	// atomicSymlinkImpl will fail (MkdirAll or Symlink permission denied)
	err := PromptAndSymlink(&buf, src, dst)
	require.NoError(t, err)
	assert.Equal(t, []string{"ln", "-sf", src, dst}, lnArgs)
}

func TestPromptAndSymlinkUserDeclines(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("n\n")
	sudoRun = func(_ []string) error { t.Fatal("sudo should not be called"); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	dir := nonWritableDir(t)
	var buf bytes.Buffer
	err := PromptAndSymlink(&buf, "src.env", filepath.Join(dir, "active.env"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// --- PromptAndMkdirAll ---

func TestPromptAndMkdirAllWritable(t *testing.T) {
	old := getuid
	getuid = func() int { return 1000 }
	defer func() { getuid = old }()

	base := t.TempDir()
	dir := filepath.Join(base, "sub", "dir")
	var buf bytes.Buffer
	require.NoError(t, PromptAndMkdirAll(&buf, dir))
	assert.DirExists(t, dir)
	assert.Empty(t, buf.String())
}

func TestPromptAndMkdirAllUserConfirms(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("y\n")
	var mkArgs []string
	sudoRun = func(args []string) error { mkArgs = args; return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	dir := nonWritableDir(t)
	target := filepath.Join(dir, "nim-cache")
	var buf bytes.Buffer
	require.NoError(t, PromptAndMkdirAll(&buf, target))
	assert.Contains(t, buf.String(), "warning:")
	assert.Equal(t, []string{"mkdir", "-p", target}, mkArgs)
}

func TestPromptAndMkdirAllUserDeclines(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("n\n")
	sudoRun = func(_ []string) error { t.Fatal("sudo should not be called"); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	dir := nonWritableDir(t)
	var buf bytes.Buffer
	err := PromptAndMkdirAll(&buf, filepath.Join(dir, "nim-cache"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// --- PromptAndPrepareNIMCache ---

func TestPromptAndPrepareNIMCacheAlreadyReady(t *testing.T) {
	// Create a dir owned by the current user's primary group with rwx.
	// nimCacheReady checks GID=0 specifically, so this won't be "ready" unless
	// running as root. We test the ready=false → prompt path via the user-confirms
	// and user-declines tests below. Here we just verify the ready path returns nil
	// by directly calling nimCacheReady on a dir that satisfies the check.
	// Since we can't reliably create a GID=0 dir in tests, we verify the negative:
	// a plain temp dir is not ready (GID != 0 unless running as root).
	dir := t.TempDir()
	if nimCacheReady(dir) {
		t.Skip("running as root or GID=0 — skip not-ready assertion")
	}
	assert.False(t, nimCacheReady(dir))
}

func TestPromptAndPrepareNIMCacheWritableUserConfirms(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("y\n")
	var calls [][]string
	sudoRun = func(args []string) error { calls = append(calls, args); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	base := t.TempDir()
	dir := filepath.Join(base, "nim-cache")
	var buf bytes.Buffer
	require.NoError(t, PromptAndPrepareNIMCache(&buf, dir))
	assert.Contains(t, buf.String(), "warning:")
	// Should have run chgrp and chmod (mkdir skipped — writable dir).
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"chgrp", "-R", "0", dir}, calls[0])
	assert.Equal(t, []string{"chmod", "-R", "777", dir}, calls[1])
}

func TestPromptAndPrepareNIMCacheNeedsRootUserConfirms(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("y\n")
	var calls [][]string
	sudoRun = func(args []string) error { calls = append(calls, args); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	nonWritable := nonWritableDir(t)
	target := filepath.Join(nonWritable, "nim-cache")
	var buf bytes.Buffer
	require.NoError(t, PromptAndPrepareNIMCache(&buf, target))
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"mkdir", "-p", target}, calls[0])
	assert.Equal(t, []string{"chgrp", "-R", "0", target}, calls[1])
	assert.Equal(t, []string{"chmod", "-R", "777", target}, calls[2])
}

func TestPromptAndPrepareNIMCacheUserDeclines(t *testing.T) {
	oldGetuid := getuid
	oldStdin := stdinR
	oldSudo := sudoRun
	getuid = func() int { return 1000 }
	stdinR = strings.NewReader("n\n")
	sudoRun = func(_ []string) error { t.Fatal("sudo should not be called"); return nil }
	defer func() { getuid = oldGetuid; stdinR = oldStdin; sudoRun = oldSudo }()

	dir := nonWritableDir(t)
	var buf bytes.Buffer
	err := PromptAndPrepareNIMCache(&buf, filepath.Join(dir, "nim-cache"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestRefreshNIMCachePermsDirNotExist(t *testing.T) {
	// When the directory does not exist, RefreshNIMCachePerms is a no-op.
	err := RefreshNIMCachePerms(filepath.Join(t.TempDir(), "nonexistent"))
	assert.NoError(t, err)
}

func TestRefreshNIMCachePermsSuccess(t *testing.T) {
	dir := t.TempDir()
	var cmds []string
	old := sudoRun
	sudoRun = func(args []string) error { cmds = append(cmds, args[0]); return nil }
	defer func() { sudoRun = old }()

	err := RefreshNIMCachePerms(dir)
	require.NoError(t, err)
	// Should have run chgrp then chmod.
	require.Len(t, cmds, 2)
	assert.Equal(t, "chgrp", cmds[0])
	assert.Equal(t, "chmod", cmds[1])
}

func TestRefreshNIMCachePermsChgrpError(t *testing.T) {
	dir := t.TempDir()
	old := sudoRun
	sudoRun = func(args []string) error {
		if args[0] == "chgrp" {
			return fmt.Errorf("chgrp failed")
		}
		return nil
	}
	defer func() { sudoRun = old }()

	err := RefreshNIMCachePerms(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chgrp failed")
}
