package shell

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleLineReaderReadsLines(t *testing.T) {
	t.Parallel()

	r := NewSimpleLineReader(strings.NewReader("hello\nworld\n"))

	line, err := r.ReadLine("prompt> ")
	require.NoError(t, err)
	assert.Equal(t, "hello", line)

	line, err = r.ReadLine("prompt> ")
	require.NoError(t, err)
	assert.Equal(t, "world", line)

	_, err = r.ReadLine("prompt> ")
	assert.Equal(t, io.EOF, err)

	assert.NoError(t, r.Close())
}

func TestSimpleLineReaderPromptIgnored(t *testing.T) {
	t.Parallel()

	r := NewSimpleLineReader(strings.NewReader("test\n"))

	// Different prompts, same input — prompt is display-only
	line, err := r.ReadLine("first> ")
	require.NoError(t, err)
	assert.Equal(t, "test", line)
}

func TestShellHostUsesLineReader(t *testing.T) {
	t.Parallel()

	var readLines []string
	mockReader := &mockLineReader{
		lines: []string{"echo hello", "exit"},
		readLines: &readLines,
	}

	var capturedCmd string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		capturedCmd = cmd
		return "hello\n", "", 0, nil
	}

	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{"echo": true},
		ShellExec:   exec,
		LineReader:  mockReader,
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
		GitBranchFn: func(string) string { return "" },
	})

	err := host.Run(context.Background())
	assert.ErrorIs(t, err, ErrExit)
	assert.Equal(t, "echo hello", capturedCmd)
}

// mockLineReader is a test double for LineReader.
type mockLineReader struct {
	lines     []string
	pos       int
	readLines *[]string
}

func (m *mockLineReader) ReadLine(_ string) (string, error) {
	if m.pos >= len(m.lines) {
		return "", io.EOF
	}
	line := m.lines[m.pos]
	m.pos++
	if m.readLines != nil {
		*m.readLines = append(*m.readLines, line)
	}
	return line, nil
}

func (m *mockLineReader) Close() error        { return nil }
func (m *mockLineReader) HandlesPrompt() bool { return false }
