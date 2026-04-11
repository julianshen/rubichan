//go:build !windows

package checkpoint

import (
	"os"
	"syscall"
)

// isProcessAlive returns true if the given PID corresponds to a running process.
// On Unix, signal(0) checks process existence without sending a real signal.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
