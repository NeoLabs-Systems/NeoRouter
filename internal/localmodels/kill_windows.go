//go:build windows

package localmodels

import (
	"context"
	"strconv"
)

// killPID ends a process this one did not start, used to clean up after a
// crashed NeoRouter.
func killPID(pid int) {
	_, _ = runCommand(context.Background(), "taskkill", "/F", "/PID", strconv.Itoa(pid))
}
