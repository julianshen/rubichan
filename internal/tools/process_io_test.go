package tools_test

import (
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipeProcessIOWriteAndRead(t *testing.T) {
	cmd := exec.Command("cat")
	pio, err := tools.NewPipeProcessIO(cmd)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()

	_, err = pio.Write([]byte("hello\n"))
	require.NoError(t, err)

	buf := make([]byte, 64)
	time.Sleep(50 * time.Millisecond)
	n, err := pio.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(buf[:n]))

	require.NoError(t, pio.Close())
}

func TestPipeProcessIOCloseSignalsEOF(t *testing.T) {
	cmd := exec.Command("cat")
	pio, err := tools.NewPipeProcessIO(cmd)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	require.NoError(t, pio.Close())
	_ = cmd.Wait()

	buf := make([]byte, 64)
	_, err = pio.Read(buf)
	assert.ErrorIs(t, err, io.EOF)
}

func TestPipeProcessIOCapturesStderr(t *testing.T) {
	// Use sh -c to write to stderr, which should be merged into stdout
	cmd := exec.Command("sh", "-c", "echo error_output >&2")
	pio, err := tools.NewPipeProcessIO(cmd)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Wait() }()

	// Give the process time to produce output
	time.Sleep(100 * time.Millisecond)

	buf := make([]byte, 64)
	n, err := pio.Read(buf)
	require.NoError(t, err)
	assert.Contains(t, string(buf[:n]), "error_output")

	require.NoError(t, pio.Close())
}

func TestNewPipeProcessIOReturnsErrorAfterStart(t *testing.T) {
	// If cmd has already been started, StdinPipe/StdoutPipe will fail.
	cmd := exec.Command("cat")
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()

	_, err := tools.NewPipeProcessIO(cmd)
	assert.Error(t, err)
}

func TestProcessIOInterfaceCompliance(t *testing.T) {
	// Verify PipeProcessIO satisfies the ProcessIO interface at compile time.
	cmd := exec.Command("cat")
	pio, err := tools.NewPipeProcessIO(cmd)
	require.NoError(t, err)

	var _ tools.ProcessIO = pio

	// Clean up — don't start the command, just verify interface compliance
}
