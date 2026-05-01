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
func (b *withheldErrorBuffer) Add(class errorclass.ErrorClass, err error) {
	b.err = &WithheldError{Class: class, Err: err}
}

// HasUnrecovered is true between Add and either MarkRecovered or Clear.
func (b *withheldErrorBuffer) HasUnrecovered() bool {
	return b.err != nil && !b.err.Recovered
}

// MarkRecovered marks the pending error as recovered so HasUnrecovered
// returns false. The error remains in the buffer until Clear is called.
func (b *withheldErrorBuffer) MarkRecovered(class errorclass.ErrorClass) {
	if b.err != nil && b.err.Class == class {
		b.err.Recovered = true
	}
}

// Clear discards the pending error so stale state doesn't leak across turns.
func (b *withheldErrorBuffer) Clear() {
	b.err = nil
}

// LastUnrecovered returns the pending error for surfacing after recovery exhausts.
func (b *withheldErrorBuffer) LastUnrecovered() (WithheldError, bool) {
	if b.err != nil && !b.err.Recovered {
		return *b.err, true
	}
	return WithheldError{}, false
}
