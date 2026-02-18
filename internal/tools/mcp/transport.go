package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// Transport abstracts the connection to an MCP server.
type Transport interface {
	Send(ctx context.Context, msg any) error
	Receive(ctx context.Context, result any) error
	Close() error
}

// Compile-time check: StdioTransport implements Transport.
var _ Transport = (*StdioTransport)(nil)

// StdioTransport communicates with an MCP server via stdin/stdout of a child process.
type StdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

// NewStdioTransport spawns a child process and sets up JSON-RPC over stdin/stdout.
func NewStdioTransport(command string, args []string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process %q: %w", command, err)
	}

	return &StdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
	}, nil
}

// Send marshals the message as JSON and writes it as a single line to stdin.
func (t *StdioTransport) Send(_ context.Context, msg any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// Receive reads the next JSON line from stdout and unmarshals it.
func (t *StdioTransport) Receive(_ context.Context, result any) error {
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return fmt.Errorf("read from stdout: %w", err)
		}
		return io.EOF
	}

	if err := json.Unmarshal(t.scanner.Bytes(), result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// Close shuts down the child process.
func (t *StdioTransport) Close() error {
	t.stdin.Close()
	return t.cmd.Wait()
}
