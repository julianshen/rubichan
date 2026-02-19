package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
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
// Lines from stdout are read in a background goroutine so that Receive respects
// context cancellation.
type StdioTransport struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	lineCh    chan lineResult
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

// lineResult carries a single line or error from the scanner goroutine.
type lineResult struct {
	data []byte
	err  error
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

	lineCh := make(chan lineResult, 16)
	done := make(chan struct{})
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(stdout)
		// MCP responses (especially tool results with large file contents)
		// can exceed the default 64KB buffer. Use 1MB.
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			// Copy bytes — scanner reuses its buffer.
			data := make([]byte, len(scanner.Bytes()))
			copy(data, scanner.Bytes())
			select {
			case lineCh <- lineResult{data: data}:
			case <-done:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lineCh <- lineResult{err: err}:
			case <-done:
			}
		}
	}()

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		lineCh: lineCh,
		done:   done,
	}, nil
}

// Send marshals the message as JSON and writes it as a single line to stdin.
// The write is performed in a goroutine so the call respects context cancellation.
func (t *StdioTransport) Send(ctx context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	type writeResult struct{ err error }
	ch := make(chan writeResult, 1)
	go func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		_, werr := t.stdin.Write(data)
		ch <- writeResult{err: werr}
	}()

	select {
	case wr := <-ch:
		if wr.err != nil {
			return fmt.Errorf("write to stdin: %w", wr.err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive reads the next JSON line from stdout and unmarshals it.
// The call is cancelled when ctx is done.
func (t *StdioTransport) Receive(ctx context.Context, result any) error {
	select {
	case lr, ok := <-t.lineCh:
		if !ok {
			return io.EOF
		}
		if lr.err != nil {
			return fmt.Errorf("read from stdout: %w", lr.err)
		}
		if err := json.Unmarshal(lr.data, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close shuts down the child process. It closes stdin, signals the scanner
// goroutine to exit, and waits up to 5 seconds for the process to exit.
// If the process doesn't exit in time, it is killed. Close is safe to call
// multiple times.
func (t *StdioTransport) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		close(t.done)
		t.stdin.Close()

		waitCh := make(chan error, 1)
		go func() { waitCh <- t.cmd.Wait() }()

		select {
		case err := <-waitCh:
			closeErr = err
		case <-time.After(5 * time.Second):
			_ = t.cmd.Process.Kill()
			closeErr = <-waitCh
		}
	})
	return closeErr
}
