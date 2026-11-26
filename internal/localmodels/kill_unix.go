//go:build !windows

package localmodels

import "syscall"

// killPID ends a process this one did not start, used to clean up after a
// crashed NeoRouter.
func killPID(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
