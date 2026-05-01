package agent

import (
	"sync"

	"github.com/julianshen/rubichan/internal/agent/errorclass"
)

type WithheldError struct {
	Class     errorclass.ErrorClass
	Err       error
	Recovered bool
}

type withheldErrorBuffer struct {
	mu     sync.Mutex
	errors []WithheldError
}

func (b *withheldErrorBuffer) Add(class errorclass.ErrorClass, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errors = append(b.errors, WithheldError{Class: class, Err: err})
}

func (b *withheldErrorBuffer) HasUnrecovered() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.errors {
		if !e.Recovered {
			return true
		}
	}
	return false
}

func (b *withheldErrorBuffer) MarkRecovered(class errorclass.ErrorClass) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.errors {
		if b.errors[i].Class == class && !b.errors[i].Recovered {
			b.errors[i].Recovered = true
		}
	}
}

func (b *withheldErrorBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errors = nil
}

func (b *withheldErrorBuffer) LastUnrecovered() *WithheldError {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.errors) - 1; i >= 0; i-- {
		if !b.errors[i].Recovered {
			return &b.errors[i]
		}
	}
	return nil
}
