//go:build unix

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func startInteractiveSignalHandler(cfgDir, sessionLogPath string, cancel context.CancelCauseFunc) func() {
	sigCh := make(chan os.Signal, 1)
	stopCh := make(chan struct{})
	var stopOnce sync.Once

	// Intercept SIGTERM (kill, Docker stop, systemd) and SIGQUIT (diagnostic dump).
	// SIGINT is handled by Bubble Tea. On first signal, persist a diagnostic dump
	// and cancel the run context so defers (LSP shutdown, DB close) run cleanly.
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGQUIT)

	stop := func() {
		stopOnce.Do(func() {
			signal.Stop(sigCh)
			close(stopCh)
		})
	}

	go func() {
		select {
		case sig := <-sigCh:
			stop()
			path, err := writeDiagnosticDump(cfgDir, sig, sessionLogPath)
			if err != nil {
				log.Printf("failed to write %s diagnostic dump: %v", sig.String(), err)
			} else {
				log.Printf("wrote %s diagnostic dump to %s", sig.String(), path)
			}
			exitCode := 128 + int(sig.(syscall.Signal))
			cancel(&interactiveSignalAbort{name: sig.String(), exitCode: exitCode})
		case <-stopCh:
		}
	}()

	return stop
}
