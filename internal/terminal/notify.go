package terminal

import (
	"fmt"
	"io"
)

// Notify sends an OSC 9 desktop notification with the given message.
func Notify(w io.Writer, message string) {
	fmt.Fprintf(w, "\x1b]9;%s\x07", message)
}
