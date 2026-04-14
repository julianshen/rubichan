package provider_test

import (
	"io"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchBody_KillsStaleStream(t *testing.T) {
	// Source writes one byte then hangs forever.
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("x"))
		// Never write again, never close — simulates a stalled TCP connection.
		time.Sleep(2 * time.Second) // longer than the test's kill timer
		pw.Close()
	}()

	cfg := provider.WatchdogConfig{
		WarnAfter: 40 * time.Millisecond,
		KillAfter: 100 * time.Millisecond,
	}
	warned := make(chan struct{}, 1)
	killed := make(chan struct{}, 1)
	onWarn := func() {
		select {
		case warned <- struct{}{}:
		default:
		}
	}
	onKill := func() {
		select {
		case killed <- struct{}{}:
		default:
		}
	}

	watched := provider.WatchBody(pr, cfg, onWarn, onKill)
	defer watched.Close()

	// Read the one byte that came through.
	buf := make([]byte, 1)
	n, err := watched.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Next read blocks, then returns ErrStreamError once the kill timer fires.
	_, err = watched.Read(buf)
	require.Error(t, err)

	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrStreamError, pe.Kind)
	assert.True(t, pe.IsRetryable())

	// Both callbacks should have fired.
	select {
	case <-warned:
	case <-time.After(500 * time.Millisecond):
		t.Error("onWarn was not called")
	}
	select {
	case <-killed:
	case <-time.After(500 * time.Millisecond):
		t.Error("onKill was not called")
	}
}

func TestWatchBody_ActivityResetsTimers(t *testing.T) {
	// Source writes a byte every 30ms for 200ms total — total time exceeds
	// the 100ms kill timer, but activity keeps resetting it.
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for i := 0; i < 6; i++ {
			_, _ = pw.Write([]byte{byte('a' + i)})
			time.Sleep(30 * time.Millisecond)
		}
	}()

	cfg := provider.WatchdogConfig{
		WarnAfter: 40 * time.Millisecond,
		KillAfter: 100 * time.Millisecond,
	}
	watched := provider.WatchBody(pr, cfg, nil, nil)
	defer watched.Close()

	// Read all 6 bytes — should succeed without a stall.
	got, err := io.ReadAll(watched)
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(got))
}

func TestWatchBody_PassesThroughClean(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_, _ = pw.Write([]byte("hello world"))
	}()

	cfg := provider.WatchdogConfig{
		WarnAfter: 100 * time.Millisecond,
		KillAfter: 500 * time.Millisecond,
	}
	watched := provider.WatchBody(pr, cfg, nil, nil)
	defer watched.Close()

	got, err := io.ReadAll(watched)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(got))
}
