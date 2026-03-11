//go:build !unix

package main

import "context"

func startInteractiveSignalHandler(_ string, _ string, _ context.CancelCauseFunc) func() {
	return func() {}
}
