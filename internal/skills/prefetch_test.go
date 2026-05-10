package skills

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefetchHandleLifecycle(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")
	assert.Equal(t, PrefetchStatePending, ph.State())

	// Simulate async load.
	go func() {
		time.Sleep(10 * time.Millisecond)
		ph.Settle(&Skill{Manifest: &SkillManifest{Name: "test-skill"}}, nil)
	}()

	// Wait for settlement.
	require.Eventually(t, func() bool {
		return ph.State() == PrefetchStateSettled
	}, time.Second, 10*time.Millisecond)

	// Consume the settled skill.
	sk, err := ph.Consume()
	require.NoError(t, err)
	assert.Equal(t, "test-skill", sk.Manifest.Name)
	assert.Equal(t, PrefetchStateConsumed, ph.State())

	// Second consume returns error.
	_, err = ph.Consume()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already consumed")
}

func TestPrefetchHandleError(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")

	ph.Settle(nil, assert.AnError)

	assert.Equal(t, PrefetchStateError, ph.State())

	_, err := ph.Consume()
	assert.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestPrefetchHandleCancel(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")

	ph.Cancel()

	assert.Equal(t, PrefetchStateError, ph.State())

	_, err := ph.Consume()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestPrefetchHandleWait(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")

	// Settle after a short delay.
	go func() {
		time.Sleep(20 * time.Millisecond)
		ph.Settle(&Skill{Manifest: &SkillManifest{Name: "test-skill"}}, nil)
	}()

	// Wait should return nil when settled.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := ph.Wait(ctx)
	require.NoError(t, err)
	assert.Equal(t, PrefetchStateSettled, ph.State())
}

func TestPrefetchHandleWaitTimeout(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")

	// Wait with a short timeout should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := ph.Wait(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPrefetchHandleDoubleSettle(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")

	ph.Settle(&Skill{Manifest: &SkillManifest{Name: "test-skill"}}, nil)

	// Second settle should be ignored.
	ph.Settle(&Skill{Manifest: &SkillManifest{Name: "other"}}, assert.AnError)

	sk, err := ph.Consume()
	require.NoError(t, err)
	assert.Equal(t, "test-skill", sk.Manifest.Name)
}

func TestPrefetchHandleDoubleCancel(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")

	ph.Cancel()
	ph.Cancel() // should not panic

	assert.Equal(t, PrefetchStateError, ph.State())
}
