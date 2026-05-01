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

// Add stores a new recoverable error, replacing any previous one.
// Previous errors are discarded because recovery handles one at a time.
func (b *withheldErrorBuffer) Add(class errorclass.ErrorClass, err error) {
	b.err = &WithheldError{Class: class, Err: err}
}

// HasUnrecovered reports whether a recoverable error is pending.
func (b *withheldErrorBuffer) HasUnrecovered() bool {
	return b.err != nil && !b.err.Recovered
}

// MarkRecovered clears the pending error so the loop can proceed cleanly.
func (b *withheldErrorBuffer) MarkRecovered(errorclass.ErrorClass) {
	if b.err != nil {
		b.err.Recovered = true
	}
}

// Clear discards the pending error. Called after the error is surfaced
// to consumers so stale state doesn't leak across turns.
func (b *withheldErrorBuffer) Clear() {
	b.err = nil
}

// LastUnrecovered returns the pending error if it hasn't been recovered.
func (b *withheldErrorBuffer) LastUnrecovered() (WithheldError, bool) {
	if b.err != nil && !b.err.Recovered {
		return *b.err, true
	}
	return WithheldError{}, false
}
