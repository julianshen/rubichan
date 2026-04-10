package terminal

import (
	"fmt"
	"io"
	"net/url"
	"strings"
)

// SetWorkingDirectory emits an OSC 7 escape sequence to notify the terminal of the current working directory.
// absPath must be an absolute filesystem path.
func SetWorkingDirectory(w io.Writer, absPath string) {
	// Build a proper file URL: encode each path segment individually to preserve slashes.
	parts := strings.Split(absPath, "/")
	encoded := make([]string, len(parts))
	for i, p := range parts {
		encoded[i] = url.PathEscape(p)
	}
	// Strip leading empty string from the leading slash to avoid triple slash becoming four.
	path := strings.Join(encoded, "/")
	path = strings.TrimPrefix(path, "/")
	fmt.Fprintf(w, "\x1b]7;file:///%s\x07", path)
}
