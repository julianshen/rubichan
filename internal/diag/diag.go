// Package diag owns the CLI's process-level diagnostics: the session log,
// the structured event log, and the stack dumps written on signal or panic.
//
// It is infrastructure rather than agent behaviour, which is why it lives
// outside cmd/: main.go's job is composition, and file layout, permissions
// and log-writer swapping are none of its business. Extracted from
// cmd/rubichan/main.go as the first slice of the redesign's Phase 3.
package diag

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/session"
)

var (
	activeSessionLogMu   sync.RWMutex
	activeSessionLogPath string
)

// SetActiveSessionLogPath records the session log's location so dumps
// written later — including from a signal handler on another goroutine —
// can point an operator at it.
func SetActiveSessionLogPath(path string) {
	activeSessionLogMu.Lock()
	defer activeSessionLogMu.Unlock()
	activeSessionLogPath = path
}

// ActiveSessionLogPath returns the path recorded by SetActiveSessionLogPath,
// or the empty string when no session log is open.
func ActiveSessionLogPath() string {
	activeSessionLogMu.RLock()
	defer activeSessionLogMu.RUnlock()
	return activeSessionLogPath
}

// SessionLogger redirects the standard logger to a per-session file and
// restores the previous writer on Close.
type SessionLogger struct {
	file       *os.File
	path       string
	prevWriter io.Writer
	prevFlags  int
}

// Path returns the session log's location on disk.
func (sl *SessionLogger) Path() string {
	if sl == nil {
		return ""
	}
	return sl.path
}

// EventLogger writes structured session events as JSONL.
type EventLogger struct {
	file *os.File
	path string
}

// Path returns the event log's location on disk.
func (el *EventLogger) Path() string {
	if el == nil {
		return ""
	}
	return el.path
}

// LogFileSuffix builds the timestamp+pid suffix that keeps concurrent
// rubichan processes from colliding on a log filename.
func LogFileSuffix(now time.Time) string {
	return fmt.Sprintf("%s-%d", now.UTC().Format("20060102-150405.000000000"), os.Getpid())
}

// CaptureAllStacks returns a goroutine dump for every goroutine, growing the
// buffer until the dump fits or 16MiB is reached — a truncated dump is worse
// than a large one when diagnosing a deadlock.
func CaptureAllStacks() []byte {
	buf := make([]byte, 1<<20)
	for len(buf) <= 16<<20 {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, len(buf)*2)
	}
	n := runtime.Stack(buf, true)
	return buf[:n]
}

// WriteStackDump writes header followed by a full goroutine dump into
// cfgDir/logs/fileName and returns the path written.
func WriteStackDump(cfgDir, fileName, header string) (string, error) {
	logDir := filepath.Join(cfgDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return "", fmt.Errorf("creating log directory: %w", err)
	}

	path := filepath.Join(logDir, fileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("opening dump file: %w", err)
	}
	defer f.Close()

	if _, err := io.WriteString(f, header); err != nil {
		return "", fmt.Errorf("writing dump header: %w", err)
	}
	if _, err := f.Write(CaptureAllStacks()); err != nil {
		return "", fmt.Errorf("writing dump stack: %w", err)
	}

	return path, nil
}

// StartSessionLogger opens a new session log under cfgDir/logs and points the
// standard logger at it, mirroring to stderr when asked. The file is opened
// O_EXCL so two processes cannot share one log.
func StartSessionLogger(cfgDir string, mirrorToStderr bool) (*SessionLogger, error) {
	logDir := filepath.Join(cfgDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	path := filepath.Join(logDir, fmt.Sprintf("rubichan-%s.log", LogFileSuffix(time.Now())))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening session log: %w", err)
	}

	sl := &SessionLogger{
		file:       f,
		path:       path,
		prevWriter: log.Writer(),
		prevFlags:  log.Flags(),
	}
	SetActiveSessionLogPath(path)
	logWriter := io.Writer(f)
	if mirrorToStderr {
		logWriter = io.MultiWriter(os.Stderr, f)
	}
	log.SetOutput(logWriter)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)
	log.Printf("rubichan session log started: %s", path)
	return sl, nil
}

// Close restores the previous log writer and flags, then closes the file.
func (sl *SessionLogger) Close() error {
	if sl == nil {
		return nil
	}
	log.Printf("rubichan session log finished")
	log.SetOutput(sl.prevWriter)
	log.SetFlags(sl.prevFlags)
	return sl.file.Close()
}

// StartEventLogger opens the JSONL event log at path. A blank path means the
// caller did not ask for one, and yields a nil logger rather than an error.
func StartEventLogger(path string) (*EventLogger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating event log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening event log: %w", err)
	}
	return &EventLogger{file: f, path: path}, nil
}

// Close closes the event log file.
func (el *EventLogger) Close() error {
	if el == nil {
		return nil
	}
	return el.file.Close()
}

// BuildEventSink centralizes interactive/headless session event wiring.
// Human-readable log mirroring is intentionally debug-only; when callers
// request only --event-log, events are written to JSONL without also being
// mirrored through the standard logger.
func BuildEventSink(structuredEventLog *EventLogger, debug bool) session.MultiSink {
	var sink session.MultiSink
	if debug {
		sink = append(sink, session.NewLogSink(log.Printf))
	}
	if structuredEventLog != nil {
		sink = append(sink, session.NewJSONLSink(structuredEventLog.file))
	}
	return sink
}

// WriteDiagnosticDump records a goroutine dump taken in response to a signal.
func WriteDiagnosticDump(cfgDir string, sig os.Signal, sessionLogPath string) (string, error) {
	now := time.Now()
	header := fmt.Sprintf(
		"timestamp: %s\nsignal: %s\nsession_log: %s\n\n",
		now.UTC().Format(time.RFC3339Nano),
		sig.String(),
		sessionLogPath,
	)
	return WriteStackDump(cfgDir, fmt.Sprintf("diagnostic-%s-%s.log", strings.ToLower(sig.String()), LogFileSuffix(now)), header)
}

// WritePanicDump records a goroutine dump taken while unwinding a panic.
func WritePanicDump(cfgDir string, recovered any, sessionLogPath string) (string, error) {
	now := time.Now()
	header := fmt.Sprintf(
		"timestamp: %s\npanic: %v\nsession_log: %s\n\n",
		now.UTC().Format(time.RFC3339Nano),
		recovered,
		sessionLogPath,
	)
	return WriteStackDump(cfgDir, fmt.Sprintf("panic-%s.log", LogFileSuffix(now)), header)
}
