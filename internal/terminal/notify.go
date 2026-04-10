package terminal

import (
	"fmt"
	"io"
	"strings"
)

// Notify sends an OSC 9 desktop notification with the given message.
// Control characters (BEL, ESC, etc.) are stripped to prevent escape sequence injection.
func Notify(w io.Writer, message string) {
	safe := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, message)
	fmt.Fprintf(w, "\x1b]9;%s\x07", safe)
}
