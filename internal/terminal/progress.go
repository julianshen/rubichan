package terminal

import (
	"fmt"
	"io"
)

// ProgressState represents the state of the terminal progress bar.
type ProgressState int

const (
	ProgressHidden        ProgressState = 0
	ProgressNormal        ProgressState = 1
	ProgressError         ProgressState = 2
	ProgressIndeterminate ProgressState = 3
	ProgressWarning       ProgressState = 4
)

// SetProgress writes an OSC 9;4 escape sequence to set the progress bar state and percentage.
// percent is clamped to [0, 100].
func SetProgress(w io.Writer, state ProgressState, percent int) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	fmt.Fprintf(w, "\x1b]9;4;%d;%d\x07", int(state), percent)
}

// ClearProgress hides the progress bar.
func ClearProgress(w io.Writer) {
	SetProgress(w, ProgressHidden, 0)
}
