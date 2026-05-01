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
// the given error class. The error stays in the buffer until Clear so
// LastUnrecovered can still report it if a later check is needed.
func (b *withheldErrorBuffer) MarkRecovered(class errorclass.ErrorClass) {
	if b.err != nil && b.err.Class == class {
		b.err.Recovered = true
	}
}

// Clear resets the buffer before each new turn so a recovered error from
// a previous turn is not mistakenly surfaced again.
func (b *withheldErrorBuffer) Clear() {
	b.err = nil
}

// LastUnrecovered surfaces the withheld error when recovery attempts are
// exhausted. The caller emits this as the final error so the user sees
// the root cause instead of a generic "retry exhausted" message.
func (b *withheldErrorBuffer) LastUnrecovered() (WithheldError, bool) {
	if b.err != nil && !b.err.Recovered {
		return *b.err, true
	}
	return WithheldError{}, false
}
