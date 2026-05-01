package agent

import (
	"github.com/julianshen/rubichan/internal/agent/errorclass"
)

// WithheldError tracks a single recoverable error that has been withheld
// from consumers while recovery is attempted.
type WithheldError struct {
	Class     errorclass.ErrorClass
	Err       error
	Recovered bool
}

// withheldErrorBuffer holds recoverable errors that are withheld from
// consumers while the loop attempts recovery. It is NOT safe for concurrent
// use; the runLoop goroutine is the sole accessor.
type withheldErrorBuffer struct {
	errors []WithheldError
}

func (b *withheldErrorBuffer) Add(class errorclass.ErrorClass, err error) {
	b.errors = append(b.errors, WithheldError{Class: class, Err: err})
}

func (b *withheldErrorBuffer) HasUnrecovered() bool {
	for _, e := range b.errors {
		if !e.Recovered {
			return true
		}
	}
	return false
}

// MarkRecovered marks the most recent unrecovered error of the given class
// as recovered. Only the last matching error is marked.
func (b *withheldErrorBuffer) MarkRecovered(class errorclass.ErrorClass) {
	for i := len(b.errors) - 1; i >= 0; i-- {
		if b.errors[i].Class == class && !b.errors[i].Recovered {
			b.errors[i].Recovered = true
			return
		}
	}
}

func (b *withheldErrorBuffer) Clear() {
	b.errors = nil
}

// LastUnrecovered returns the most recent unrecovered error as a value copy.
func (b *withheldErrorBuffer) LastUnrecovered() (WithheldError, bool) {
	for i := len(b.errors) - 1; i >= 0; i-- {
		if !b.errors[i].Recovered {
			return b.errors[i], true
		}
	}
	return WithheldError{}, false
}
