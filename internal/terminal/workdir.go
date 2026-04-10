package terminal

import (
	"fmt"
	"io"
	"net/url"
)

// SetWorkingDirectory emits an OSC 7 escape sequence to notify the terminal of the current working directory.
// absPath must be an absolute filesystem path.
func SetWorkingDirectory(w io.Writer, absPath string) {
	u := &url.URL{Scheme: "file", Path: absPath}
	fmt.Fprintf(w, "\x1b]7;%s\x07", u.String())
}
