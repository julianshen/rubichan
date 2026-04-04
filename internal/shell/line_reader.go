package shell

import (
	"bufio"
	"io"
)

// LineReader abstracts line input with optional completion support.
type LineReader interface {
	// ReadLine reads a line of input. The prompt is passed for libraries
	// that handle their own prompt display (e.g., readline).
	ReadLine(prompt string) (string, error)
	// Close cleans up resources.
	Close() error
	// HandlesPrompt returns true if the LineReader displays the prompt itself
	// (e.g., a readline library). When false, the caller must print the prompt.
	HandlesPrompt() bool
}

// SimpleLineReader wraps bufio.Scanner for testing and basic use (no completion).
type SimpleLineReader struct {
	scanner *bufio.Scanner
}

// NewSimpleLineReader creates a line reader from an io.Reader.
func NewSimpleLineReader(r io.Reader) *SimpleLineReader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &SimpleLineReader{scanner: s}
}

// ReadLine reads the next line. Returns io.EOF when input is exhausted.
func (lr *SimpleLineReader) ReadLine(_ string) (string, error) {
	if lr.scanner.Scan() {
		return lr.scanner.Text(), nil
	}
	if err := lr.scanner.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}

// Close is a no-op for SimpleLineReader.
func (lr *SimpleLineReader) Close() error {
	return nil
}

// HandlesPrompt returns false — the caller must print the prompt.
func (lr *SimpleLineReader) HandlesPrompt() bool {
	return false
}
