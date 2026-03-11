package lsp

import (
	"fmt"
	"io"
	"os/exec"
	"time"
)

// serverProcess wraps an os/exec.Cmd as an io.ReadWriteCloser for the JSON-RPC client.
type serverProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (s *serverProcess) Read(p []byte) (int, error) {
	return s.stdout.Read(p)
}

func (s *serverProcess) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

// Close shuts down the server process. It closes stdin to signal the process,
// waits briefly for a graceful exit, then kills if needed. Always reaps the
// child to avoid zombie processes.
func (s *serverProcess) Close() error {
	_ = s.stdin.Close()
	_ = s.stdout.Close()

	// Try to wait for graceful exit before killing.
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		_ = s.cmd.Process.Kill()
		return <-done // reap the zombie
	}
}

// spawnServer starts a language server process and returns an io.ReadWriteCloser
// that communicates via the process's stdin/stdout.
func spawnServer(cfg ServerConfig) (io.ReadWriteCloser, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	return &serverProcess{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}
