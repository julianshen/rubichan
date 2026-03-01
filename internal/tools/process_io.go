package tools

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ProcessIO abstracts read/write access to a running process.
// This allows swapping pipe-based I/O for PTY-based I/O later
// without changing callers.
type ProcessIO interface {
	// Write sends bytes to the process's standard input.
	Write(p []byte) (int, error)

	// Read receives bytes from the process's combined stdout/stderr.
	Read(p []byte) (int, error)

	// Close closes the stdin pipe, signaling EOF to the process.
	Close() error
}

// PipeProcessIO implements ProcessIO using OS pipes from exec.Cmd.
// Stderr is merged into stdout so callers receive all output through Read.
type PipeProcessIO struct {
	stdin     io.WriteCloser
	stdout    *os.File
	outWriter *os.File
}

// NewPipeProcessIO creates a PipeProcessIO by attaching stdin and stdout
// pipes to the given command and redirecting stderr to stdout. It must be
// called before cmd.Start().
func NewPipeProcessIO(cmd *exec.Cmd) (*PipeProcessIO, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	// Create our own OS-level pipe rather than using cmd.StdoutPipe().
	// cmd.StdoutPipe() returns a reader that Go's exec package closes
	// when cmd.Wait() returns, producing "file already closed" instead
	// of io.EOF. With our own OS pipe, the read end stays valid and
	// returns io.EOF once the write end is closed (when the process exits
	// and we close our copy).
	outReader, outWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	cmd.Stdout = outWriter
	cmd.Stderr = outWriter

	return &PipeProcessIO{
		stdin:     stdin,
		stdout:    outReader,
		outWriter: outWriter,
	}, nil
}

// Write sends p to the process's stdin.
func (p *PipeProcessIO) Write(data []byte) (int, error) {
	return p.stdin.Write(data)
}

// Read reads from the process's combined stdout/stderr stream.
func (p *PipeProcessIO) Read(buf []byte) (int, error) {
	return p.stdout.Read(buf)
}

// Close closes the stdin pipe (signaling EOF to the process) and the
// write end of the stdout pipe so that subsequent reads return io.EOF
// once all buffered data has been consumed.
func (p *PipeProcessIO) Close() error {
	stdinErr := p.stdin.Close()
	writerErr := p.outWriter.Close()
	if stdinErr != nil {
		return stdinErr
	}
	return writerErr
}
