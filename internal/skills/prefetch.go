package skills

import (
	"context"
	"fmt"
	"sync"
)

// PrefetchState represents the lifecycle state of a skill prefetch.
type PrefetchState int

const (
	// PrefetchStatePending means the prefetch has been initiated but not yet completed.
	PrefetchStatePending PrefetchState = iota
	// PrefetchStateSettled means the prefetch completed successfully and the skill is ready.
	PrefetchStateSettled
	// PrefetchStateConsumed means the settled skill has been retrieved by a caller.
	PrefetchStateConsumed
	// PrefetchStateError means the prefetch failed or was cancelled.
	PrefetchStateError
)

// PrefetchHandle tracks the async loading of a single skill.
// It is thread-safe and can be consumed exactly once.
type PrefetchHandle struct {
	skillName string
	state     PrefetchState
	skill     *Skill
	err       error
	mu        sync.Mutex
	closeOnce sync.Once
	done      chan struct{}
}

// NewPrefetchHandle creates a new prefetch handle for the named skill.
func NewPrefetchHandle(skillName string) *PrefetchHandle {
	return &PrefetchHandle{
		skillName: skillName,
		state:     PrefetchStatePending,
		done:      make(chan struct{}),
	}
}

// State returns the current prefetch state.
func (ph *PrefetchHandle) State() PrefetchState {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	return ph.state
}

// Settle marks the prefetch as complete with either a skill or an error.
// This must be called exactly once by the loader goroutine.
func (ph *PrefetchHandle) Settle(skill *Skill, err error) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	if ph.state != PrefetchStatePending {
		return // already settled or cancelled
	}

	ph.skill = skill
	ph.err = err
	if err != nil {
		ph.state = PrefetchStateError
	} else {
		ph.state = PrefetchStateSettled
	}
	ph.closeOnce.Do(func() { close(ph.done) })
}

// Cancel marks the prefetch as cancelled (error state).
func (ph *PrefetchHandle) Cancel() {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	if ph.state != PrefetchStatePending {
		return
	}

	ph.state = PrefetchStateError
	ph.err = fmt.Errorf("prefetch for skill %q cancelled", ph.skillName)
	ph.closeOnce.Do(func() { close(ph.done) })
}

// Consume retrieves the prefetched skill. Returns an error if the prefetch
// failed, was cancelled, or has already been consumed.
// This can be called multiple times but only the first call succeeds.
func (ph *PrefetchHandle) Consume() (*Skill, error) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	switch ph.state {
	case PrefetchStatePending:
		return nil, fmt.Errorf("prefetch for skill %q still pending", ph.skillName)
	case PrefetchStateConsumed:
		return nil, fmt.Errorf("prefetch for skill %q already consumed", ph.skillName)
	case PrefetchStateError:
		return nil, ph.err
	case PrefetchStateSettled:
		ph.state = PrefetchStateConsumed
		return ph.skill, nil
	default:
		return nil, fmt.Errorf("prefetch for skill %q in unknown state", ph.skillName)
	}
}

// Wait blocks until the prefetch settles (or is cancelled).
func (ph *PrefetchHandle) Wait(ctx context.Context) error {
	select {
	case <-ph.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
