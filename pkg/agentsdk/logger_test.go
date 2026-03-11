package agentsdk

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLoggerWarn(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer log.SetOutput(nil)
	defer log.SetFlags(log.LstdFlags)

	logger := DefaultLogger()
	logger.Warn("something %s", "happened")

	assert.True(t, strings.Contains(buf.String(), "WARN: something happened"))
}

func TestDefaultLoggerError(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer log.SetOutput(nil)
	defer log.SetFlags(log.LstdFlags)

	logger := DefaultLogger()
	logger.Error("failure: %d", 42)

	assert.True(t, strings.Contains(buf.String(), "ERROR: failure: 42"))
}

func TestLoggerInterfaceSatisfied(t *testing.T) {
	// Verify DefaultLogger returns a Logger.
	var l Logger = DefaultLogger()
	assert.NotNil(t, l)
}

// mockLogger verifies custom Logger implementations work.
type mockLogger struct {
	warns  []string
	errors []string
}

func (m *mockLogger) Warn(msg string, args ...any)  { m.warns = append(m.warns, msg) }
func (m *mockLogger) Error(msg string, args ...any) { m.errors = append(m.errors, msg) }

func TestCustomLoggerImplementation(t *testing.T) {
	ml := &mockLogger{}
	var l Logger = ml

	l.Warn("w1")
	l.Error("e1")

	assert.Equal(t, []string{"w1"}, ml.warns)
	assert.Equal(t, []string{"e1"}, ml.errors)
}
