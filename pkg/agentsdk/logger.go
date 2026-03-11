package agentsdk

import "log"

// Logger provides structured logging for the agent core. Implementations
// should be safe for concurrent use.
type Logger interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// defaultLogger wraps log.Printf as a Logger implementation.
type defaultLogger struct{}

func (defaultLogger) Warn(msg string, args ...any) {
	log.Printf("WARN: "+msg, args...)
}

func (defaultLogger) Error(msg string, args ...any) {
	log.Printf("ERROR: "+msg, args...)
}

// DefaultLogger returns a Logger that writes to the standard log package.
func DefaultLogger() Logger {
	return defaultLogger{}
}
