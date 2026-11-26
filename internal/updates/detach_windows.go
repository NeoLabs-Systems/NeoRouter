//go:build windows

package updates

import (
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP keeps the runner alive when the server is stopped.
const createNewProcessGroup = 0x00000200

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}
