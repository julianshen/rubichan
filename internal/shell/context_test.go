package shell

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextTracker_RecordAndRetrieve(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(4096)

	tracker.Record("ls -la", "file1.go\nfile2.go", 0)

	assert.Equal(t, "ls -la", tracker.LastCommand())
	assert.Equal(t, "file1.go\nfile2.go", tracker.LastOutput())
	assert.Equal(t, 0, tracker.LastExitCode())
}

func TestContextTracker_OutputTruncation(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(100) // very small limit

	longOutput := strings.Repeat("x", 5000)
	tracker.Record("cmd", longOutput, 0)

	output := tracker.LastOutput()
	assert.Less(t, len(output), 5000)
	assert.Contains(t, output, "... (truncated")
}

func TestContextTracker_ContextMessage(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(4096)

	tracker.Record("ls -la", "file1.go\nfile2.go", 0)
	msg := tracker.ContextMessage()

	assert.Contains(t, msg, "ls -la")
	assert.Contains(t, msg, "exit code 0")
	assert.Contains(t, msg, "file1.go")
}

func TestContextTracker_EmptyContextMessage(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(4096)

	msg := tracker.ContextMessage()
	assert.Empty(t, msg)
}

func TestContextTracker_FailedCommandContext(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(4096)

	tracker.Record("go test", "FAIL ...", 1)
	msg := tracker.ContextMessage()

	assert.Contains(t, msg, "exit code 1")
	assert.Contains(t, msg, "go test")
	assert.Contains(t, msg, "FAIL")
}

func TestContextTracker_Clear(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(4096)

	tracker.Record("ls", "output", 0)
	tracker.Clear()

	assert.Empty(t, tracker.ContextMessage())
}

func TestContextTracker_OverwriteOnNewRecord(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(4096)

	tracker.Record("cmd1", "output1", 0)
	tracker.Record("cmd2", "output2", 1)

	assert.Equal(t, "cmd2", tracker.LastCommand())
	assert.Equal(t, "output2", tracker.LastOutput())
	assert.Equal(t, 1, tracker.LastExitCode())
}
