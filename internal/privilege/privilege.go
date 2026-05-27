package privilege

import (
	"fmt"
	"os"
	"os/exec"
)

// injectable for tests
var (
	getuid = os.Getuid
	osExit = os.Exit
)

// RequireRoot re-executes the current binary under sudo if not already running
// as root, transparently inheriting stdin/stdout/stderr so the sudo password
// prompt behaves exactly like systemctl's privilege escalation.
// Call this at the start of any command that writes to system paths or
// manages services.
func RequireRoot() {
	if getuid() == 0 {
		return
	}
	cmd := exec.Command("sudo", append([]string{os.Args[0]}, os.Args[1:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
		return
	}
	osExit(0)
}
