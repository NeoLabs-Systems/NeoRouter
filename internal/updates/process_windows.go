//go:build windows

package updates

import "os"

// processAlive reports whether pid names a running process. Windows has no
// signal 0, so the process table is the honest check: FindProcess succeeds for
// any pid, so a failed Open is what tells us the process is gone.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = handle.Release()
	return true
}
