package lsp

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

const installTimeout = 2 * time.Minute

// TryInstall attempts to install the language server for the given language ID.
// It iterates through the configured InstallCmds, skipping those whose
// prerequisite binary (Method) is not available. Each install attempt is run
// with a timeout. Results are cached so that at most one install is attempted
// per language per session.
func (m *Manager) TryInstall(ctx context.Context, languageID string) error {
	if _, attempted := m.installAttempted.Load(languageID); attempted {
		return fmt.Errorf("install for %s already attempted this session", languageID)
	}

	cfg, err := m.registry.ConfigFor(languageID)
	if err != nil {
		return err
	}

	if len(cfg.InstallCmds) == 0 {
		m.installAttempted.Store(languageID, true)
		return fmt.Errorf("no install commands for %s", languageID)
	}

	var hints []string
	for _, ic := range cfg.InstallCmds {
		if ic.Hint != "" {
			hints = append(hints, ic.Hint)
		}
		if ic.Command == "" {
			continue
		}
		if ic.Method != "" {
			if _, err := exec.LookPath(ic.Method); err != nil {
				continue
			}
		}

		log.Printf("Installing LSP server for %s via: %s", languageID, ic.Command)

		installCtx, cancel := context.WithTimeout(ctx, installTimeout)
		cmd := exec.CommandContext(installCtx, "sh", "-c", ic.Command)
		output, err := cmd.CombinedOutput()
		cancel()

		if err == nil {
			log.Printf("LSP server for %s installed successfully", languageID)
			m.installAttempted.Store(languageID, true)
			return nil
		}
		log.Printf("Install failed for %s: %s (output: %s)", languageID, err, strings.TrimSpace(string(output)))
	}

	m.installAttempted.Store(languageID, true)

	if len(hints) > 0 {
		return fmt.Errorf("could not install LSP server for %s. Hints: %s", languageID, strings.Join(hints, "; "))
	}
	return fmt.Errorf("all install methods failed for %s", languageID)
}
