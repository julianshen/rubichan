package lsp

import (
	"fmt"
	"io"
	"os/exec"
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

func (s *serverProcess) Close() error {
	_ = s.stdin.Close()
	_ = s.stdout.Close()
	return s.cmd.Process.Kill()
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
