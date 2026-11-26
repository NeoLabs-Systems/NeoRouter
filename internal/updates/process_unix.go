//go:build !windows

package updates

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid is a running process; a process owned by
// somebody else still counts.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
