package provider

import (
	"io"
	"sync"
	"time"
)

// WatchdogConfig configures the stream idle watchdog timers.
type WatchdogConfig struct {
	// WarnAfter is the idle duration before onWarn fires. Defaults to 45s if zero.
	WarnAfter time.Duration
	// KillAfter is the idle duration before the stream is aborted. Defaults to 90s if zero.
	KillAfter time.Duration
}

func (c WatchdogConfig) warnAfter() time.Duration {
	if c.WarnAfter <= 0 {
		return 45 * time.Second
	}
	return c.WarnAfter
}

func (c WatchdogConfig) killAfter() time.Duration {
	if c.KillAfter <= 0 {
		return 90 * time.Second
	}
	return c.KillAfter
}

// WatchBody wraps body with an idle watchdog. If no bytes arrive for
// cfg.KillAfter, the underlying body is closed and the returned reader
// yields an ErrStreamError. If no bytes arrive for cfg.WarnAfter, onWarn
// fires (purely informational — the stream continues).
//
// Both timers reset on any read activity. Callers must use the returned
// io.ReadCloser in place of body; closing it cancels the watchdog.
func WatchBody(body io.ReadCloser, cfg WatchdogConfig, onWarn, onKill func()) io.ReadCloser {
	pr, pw := io.Pipe()
	activity := make(chan struct{}, 1)
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	// Pump: copy body -> pipe, signal activity on each read.
	go func() {
		defer stop()
		buf := make([]byte, 32*1024)
		for {
			n, rerr := body.Read(buf)
			if n > 0 {
				select {
				case activity <- struct{}{}:
				default:
				}
				if _, werr := pw.Write(buf[:n]); werr != nil {
					_ = body.Close()
					return
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					_ = pw.Close()
				} else {
					_ = pw.CloseWithError(rerr)
				}
				return
			}
		}
	}()

	// Watchdog: timers reset on activity; fire onWarn / onKill on idle.
	go func() {
		warnTimer := time.NewTimer(cfg.warnAfter())
		killTimer := time.NewTimer(cfg.killAfter())
		defer warnTimer.Stop()
		defer killTimer.Stop()
		warnFired := false
		for {
			select {
			case <-done:
				return
			case <-activity:
				// Drain+reset both timers.
				if !warnTimer.Stop() {
					select {
					case <-warnTimer.C:
					default:
					}
				}
				if !killTimer.Stop() {
					select {
					case <-killTimer.C:
					default:
					}
				}
				warnTimer.Reset(cfg.warnAfter())
				killTimer.Reset(cfg.killAfter())
				warnFired = false
			case <-warnTimer.C:
				if !warnFired && onWarn != nil {
					onWarn()
					warnFired = true
				}
			case <-killTimer.C:
				if onKill != nil {
					onKill()
				}
				_ = body.Close()
				stallErr := &ProviderError{
					Kind:      ErrStreamError,
					Message:   "stream stalled: no data for " + cfg.killAfter().String(),
					Retryable: true,
				}
				_ = pw.CloseWithError(stallErr)
				return
			}
		}
	}()

	return pr
}
