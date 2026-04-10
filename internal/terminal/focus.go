package terminal

import (
	"fmt"
	"io"
)

// EnableFocusEvents enables focus event reporting (Mode 1004).
func EnableFocusEvents(w io.Writer) {
	fmt.Fprint(w, "\x1b[?1004h")
}

// DisableFocusEvents disables focus event reporting.
func DisableFocusEvents(w io.Writer) {
	fmt.Fprint(w, "\x1b[?1004l")
}
