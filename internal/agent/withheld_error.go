package agent

import (
	"github.com/julianshen/rubichan/internal/agent/errorclass"
)

// WithheldError tracks a recoverable error that is being withheld from
// consumers while the loop attempts recovery. Only one error is withheld
// at a time because recovery is synchronous and single-threaded.
type WithheldError struct {
	Class     errorclass.ErrorClass
	Err       error
	Recovered bool
}

// withheldErrorBuffer tracks at most one unrecovered error at a time.
// The runLoop goroutine is the sole accessor, so no mutex is needed.
type withheldErrorBuffer struct {
	err *WithheldError
}

// Add replaces any previous error so recovery always targets the latest
// failure — stale errors from earlier attempts must not shadow new ones.
func (b *withheldErrorBuffer) Add(class errorclass.ErrorClass, err error) {
	b.err = &WithheldError{Class: class, Err: err}
}

// HasUnrecovered reports whether a recoverable error is still pending.
// The loop uses this to decide whether to surface an error or attempt
// another recovery round.
func (b *withheldErrorBuffer) HasUnrecovered() bool {
	return b.err != nil && !b.err.Recovered
}

// MarkRecovered acknowledges that the loop successfully recovered from
// the given error class. The Recovered flag is checked by HasUnrecovered
// to stop further recovery attempts; the error remains in the buffer
// until Clear so the caller can inspect it for logging or telemetry.
func (b *withheldErrorBuffer) MarkRecovered(class errorclass.ErrorClass) {
	if b.err != nil && b.err.Class == class {
		b.err.Recovered = true
	}
}

// Clear discards the pending error so stale state doesn't leak across
// recovery attempts. The caller must call this after recovery succeeds
// or exhausts, before starting a new recovery cycle.
func (b *withheldErrorBuffer) Clear() {
	b.err = nil
}

// LastUnrecovered returns the pending unrecovered error for surfacing
// after recovery exhausts. Callers typically log or emit this so the
// user sees the root cause rather than a generic retry-exhausted message.
func (b *withheldErrorBuffer) LastUnrecovered() (WithheldError, bool) {
	if b.err != nil && !b.err.Recovered {
		return *b.err, true
	}
	return WithheldError{}, false
}
