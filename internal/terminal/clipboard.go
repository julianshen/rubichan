package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
)

// CopyToClipboard sends an OSC 52 escape sequence to copy content to the terminal clipboard.
func CopyToClipboard(w io.Writer, content string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	fmt.Fprintf(w, "\x1b]52;c;%s\x1b\\", encoded)
}
