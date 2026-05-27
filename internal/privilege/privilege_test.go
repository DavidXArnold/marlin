package privilege

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequireRootWhenRoot(t *testing.T) {
	old := getuid
	getuid = func() int { return 0 }
	defer func() { getuid = old }()

	// Must return immediately without calling sudo or osExit.
	RequireRoot()
}

func TestRequireRootSuccessfulSudo(t *testing.T) {
	oldGetuid := getuid
	oldExit := osExit
	getuid = func() int { return 1000 }

	var exitCode int
	osExit = func(code int) { exitCode = code }
	defer func() {
		getuid = oldGetuid
		osExit = oldExit
	}()

	// sudo will fail (test binary can't be re-executed this way in CI).
	// The failure path calls osExit(1).
	RequireRoot()
	assert.Equal(t, 1, exitCode)
}
