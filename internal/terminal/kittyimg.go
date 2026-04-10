package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
)

const kittyChunkSize = 4096

// KittyImage transmits pngData to the terminal using the Kitty graphics protocol.
// Large payloads are automatically split into chunks.
// Nil or empty data is a no-op.
func KittyImage(w io.Writer, pngData []byte) {
	if len(pngData) == 0 {
		return
	}

	encoded := base64.StdEncoding.EncodeToString(pngData)

	if len(encoded) <= kittyChunkSize {
		// Single chunk
		fmt.Fprintf(w, "\x1b_Ga=T,f=100,m=0;%s\x1b\\", encoded)
		return
	}

	// Multiple chunks
	chunks := splitIntoChunks(encoded, kittyChunkSize)
	for i, chunk := range chunks {
		switch {
		case i == 0:
			// First chunk: include action and format headers, mark more to come
			fmt.Fprintf(w, "\x1b_Ga=T,f=100,m=1;%s\x1b\\", chunk)
		case i == len(chunks)-1:
			// Last chunk: signal end of payload
			fmt.Fprintf(w, "\x1b_Gm=0;%s\x1b\\", chunk)
		default:
			// Continuation chunk
			fmt.Fprintf(w, "\x1b_Gm=1;%s\x1b\\", chunk)
		}
	}
}

// splitIntoChunks divides s into slices of at most size characters.
func splitIntoChunks(s string, size int) []string {
	var chunks []string
	for len(s) > size {
		chunks = append(chunks, s[:size])
		s = s[size:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}
