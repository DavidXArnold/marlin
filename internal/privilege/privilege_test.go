package privilege

import (
	"fmt"
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
