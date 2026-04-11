package checkpoint

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessAliveCurrentProcess(t *testing.T) {
	assert.True(t, isProcessAlive(os.Getpid()))
}

func TestProcessAliveDeadProcess(t *testing.T) {
	// PID 999999999 is extremely unlikely to be a real process.
	assert.False(t, isProcessAlive(999999999))
}

func TestProcessAliveNegativePID(t *testing.T) {
	assert.False(t, isProcessAlive(-1))
}
