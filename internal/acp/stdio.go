package acp

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
)

// StdioTransport handles JSON-RPC communication over stdin/stdout.
type StdioTransport struct {
	stdin  io.Reader
	stdout io.Writer
	server *Server
	reader *bufio.Scanner
}

// NewStdioTransport creates a new stdio-based ACP transport.
func NewStdioTransport(stdin io.Reader, stdout io.Writer, server *Server) *StdioTransport {
	return &StdioTransport{
		stdin:  stdin,
		stdout: stdout,
		server: server,
		reader: bufio.NewScanner(stdin),
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
func (t *StdioTransport) SendMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = t.stdout.Write(append(data, '\n'))
	return err
}
