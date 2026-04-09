package acp

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"sync"
)

const (
	// maxMessageSize is the maximum size of a single JSON-RPC message (10 MB).
	// This accommodates large file reads and LLM completion outputs.
	// Note: bufio.Scanner has a hard internal limit on token size. While this
	// buffer setting helps, extremely large messages may still fail. For
	// production systems handling very large payloads, consider using
	// json.Decoder directly or implementing a chunking mechanism.
	maxMessageSize = 10 * 1024 * 1024
)

// StdioTransport handles JSON-RPC communication over stdin/stdout.
type StdioTransport struct {
	stdin  io.Reader
	stdout io.Writer
	server *Server
	reader *bufio.Scanner

	// mu protects concurrent writes to stdout from multiple goroutines.
	// This prevents interleaved JSON messages which would corrupt the wire protocol.
	mu sync.Mutex
}

// NewStdioTransport creates a new stdio-based ACP transport.
func NewStdioTransport(stdin io.Reader, stdout io.Writer, server *Server) *StdioTransport {
	scanner := bufio.NewScanner(stdin)
	// Set maximum message size to accommodate large payloads (file reads, LLM outputs)
	scanner.Buffer(make([]byte, 0, maxMessageSize), maxMessageSize)
	return &StdioTransport{
		stdin:  stdin,
		stdout: stdout,
		server: server,
		reader: scanner,
	}
}

// Start begins listening for incoming JSON-RPC messages on stdin.
func (t *StdioTransport) Start() error {
	for t.reader.Scan() {
		line := t.reader.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse and handle request
		respData, err := t.server.HandleMessage(line)
		if err != nil {
			log.Printf("error handling message: %v", err)
			continue
		}

		// Write response
		if _, err := t.stdout.Write(respData); err != nil {
			return err
		}
		if _, err := t.stdout.Write([]byte("\n")); err != nil {
			return err
		}
	}

	return t.reader.Err()
}

// SendMessage sends a JSON-RPC message to stdout.
// Uses a mutex to prevent concurrent writes which would corrupt the wire protocol.
func (t *StdioTransport) SendMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Protect concurrent writes to stdout
	t.mu.Lock()
	defer t.mu.Unlock()

	_, err = t.stdout.Write(append(data, '\n'))
	return err
}
