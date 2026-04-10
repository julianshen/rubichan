package terminal

import (
	"fmt"
	"io"
)

// BeginSync enables synchronized rendering (Mode 2026) to reduce flicker.
func BeginSync(w io.Writer) {
	fmt.Fprint(w, "\x1b[?2026h")
}

// EndSync disables synchronized rendering.
func EndSync(w io.Writer) {
	fmt.Fprint(w, "\x1b[?2026l")
}
