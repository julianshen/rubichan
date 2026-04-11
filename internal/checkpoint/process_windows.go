//go:build windows

package checkpoint

import (
	"golang.org/x/sys/windows"
)

// isProcessAlive returns true if the given PID corresponds to a running process.
// On Windows, OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION succeeds only
// if the process exists and the caller has sufficient access.
func isProcessAlive(pid int) bool {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	h, err := windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}
