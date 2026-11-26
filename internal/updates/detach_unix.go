//go:build !windows

package updates

import (
	"os/exec"
	"syscall"
)

// detach puts the runner in its own session, so it outlives the server it
// restarts and does not take the terminal's signals.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
