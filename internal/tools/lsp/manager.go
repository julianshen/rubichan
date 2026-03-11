package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// SpawnFunc creates a transport for a language server from a config.
// The default implementation spawns a child process; tests can inject a mock.
type SpawnFunc func(cfg ServerConfig) (io.ReadWriteCloser, error)

// Manager tracks language servers across the workspace. Servers are started
// lazily on first use and only if the binary is available on PATH. It also
// caches diagnostics from publishDiagnostics notifications and tracks document
// open/version state for didOpen/didChange synchronization.
type Manager struct {
	registry    *Registry
	rootURI     string
	summarizer  *Summarizer
	spawnServer SpawnFunc

	mu      sync.Mutex
	servers map[string]*serverHandle // language -> handle
	docs    map[string]int           // URI -> version (for didOpen/didChange tracking)

	diagMu sync.RWMutex
	diags  map[string][]Diagnostic // URI -> latest diagnostics
}

// serverHandle wraps a running language server.
type serverHandle struct {
	client       *Client
	capabilities ServerCapabilities
}

// NewManager creates a new manager for the given workspace root.
func NewManager(registry *Registry, rootDir string) *Manager {
	return &Manager{
		registry:    registry,
		rootURI:     pathToURI(rootDir),
		summarizer:  DefaultSummarizer(),
		spawnServer: spawnServer,
		servers:     make(map[string]*serverHandle),
		docs:        make(map[string]int),
		diags:       make(map[string][]Diagnostic),
	}
}

// SetSummarizer sets a custom summarizer for response truncation.
func (m *Manager) SetSummarizer(s *Summarizer) {
	m.summarizer = s
}

// ServerFor returns the client for the given language, starting the server if
// needed. Returns ErrServerNotInstalled if the binary is not on PATH.
func (m *Manager) ServerFor(ctx context.Context, languageID string) (*Client, *ServerCapabilities, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if handle, ok := m.servers[languageID]; ok {
		return handle.client, &handle.capabilities, nil
	}

	cfg, err := m.registry.ConfigFor(languageID)
	if err != nil {
		return nil, nil, err
	}

	if !m.registry.IsInstalled(languageID) {
		return nil, nil, fmt.Errorf("%w: %s (%s not found on PATH)", ErrServerNotInstalled, languageID, cfg.Command)
	}

	client, caps, err := m.startServer(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("start %s server: %w", languageID, err)
	}

	m.servers[languageID] = &serverHandle{
		client:       client,
		capabilities: caps,
	}

	return client, &caps, nil
}

// ServerForFile detects the language from file extension and returns the server.
func (m *Manager) ServerForFile(ctx context.Context, filePath string) (*Client, *ServerCapabilities, error) {
	lang, ok := m.registry.LanguageForFile(filePath)
	if !ok {
		return nil, nil, fmt.Errorf("no language server configured for file: %s", filePath)
	}
	return m.ServerFor(ctx, lang)
}

// DiagnosticsFor returns cached diagnostics for a file URI.
// Returns a copy so callers cannot mutate the internal cache.
func (m *Manager) DiagnosticsFor(uri string, includeWarnings bool) []Diagnostic {
	m.diagMu.RLock()
	defer m.diagMu.RUnlock()

	all := m.diags[uri]
	if includeWarnings {
		result := make([]Diagnostic, len(all))
		copy(result, all)
		return result
	}

	var errors []Diagnostic
	for _, d := range all {
		if d.Severity == SeverityError {
			errors = append(errors, d)
		}
	}
	return errors
}

// NotifyFileChanged sends didOpen or didChange to the appropriate server.
// Intended to be called whenever a file's content changes.
func (m *Manager) NotifyFileChanged(ctx context.Context, filePath string, content []byte) error {
	lang, ok := m.registry.LanguageForFile(filePath)
	if !ok {
		return nil // no server for this file type
	}

	client, _, err := m.ServerFor(ctx, lang)
	if err != nil {
		return fmt.Errorf("server for %s: %w", lang, err)
	}

	uri := pathToURI(filePath)

	m.mu.Lock()
	version, opened := m.docs[uri]
	version++
	m.docs[uri] = version
	m.mu.Unlock()

	if !opened {
		// First time seeing this file — send didOpen.
		return client.Notify(ctx, "textDocument/didOpen", DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        uri,
				LanguageID: lang,
				Version:    version,
				Text:       string(content),
			},
		})
	}

	// File already open — send didChange with full content.
	return client.Notify(ctx, "textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: string(content)},
		},
	})
}

// Shutdown gracefully stops all running servers by sending the LSP shutdown
// request followed by the exit notification, then closing the transport.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for lang, handle := range m.servers {
		if handle.client != nil {
			// Send shutdown request, then exit notification.
			_, _ = handle.client.Call(ctx, "shutdown", nil)
			_ = handle.client.Notify(ctx, "exit", nil)
			if err := handle.client.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close %s: %w", lang, err))
			}
		}
	}
	m.servers = make(map[string]*serverHandle)
	return errors.Join(errs...)
}

// EnsureFileOpen sends textDocument/didOpen if the file hasn't been opened yet.
// Must be called before position-based LSP requests to ensure the server knows
// about the file. Returns an error if the file cannot be read or the notification fails.
func (m *Manager) EnsureFileOpen(ctx context.Context, client *Client, filePath string) error {
	uri := pathToURI(filePath)

	// Mark the file as opened under the lock to prevent concurrent callers from
	// racing (TOCTOU). If the notification later fails, we revert the mark.
	m.mu.Lock()
	if _, opened := m.docs[uri]; opened {
		m.mu.Unlock()
		return nil
	}
	m.docs[uri] = 1
	m.mu.Unlock()

	content, err := os.ReadFile(filePath)
	if err != nil {
		// Revert — file couldn't be read.
		m.mu.Lock()
		delete(m.docs, uri)
		m.mu.Unlock()
		return fmt.Errorf("read file for didOpen: %w", err)
	}

	lang, _ := m.registry.LanguageForFile(filePath)

	err = client.Notify(ctx, "textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: lang,
			Version:    1,
			Text:       string(content),
		},
	})
	if err != nil {
		// Revert — notification failed.
		m.mu.Lock()
		delete(m.docs, uri)
		m.mu.Unlock()
		return fmt.Errorf("didOpen notification: %w", err)
	}

	return nil
}

// startServer spawns a language server process and performs the initialize handshake.
func (m *Manager) startServer(ctx context.Context, cfg ServerConfig) (*Client, ServerCapabilities, error) {
	proc, err := m.spawnServer(cfg)
	if err != nil {
		return nil, ServerCapabilities{}, fmt.Errorf("spawn: %w", err)
	}

	client := NewClient(proc, func(method string, params json.RawMessage) {
		if method == "textDocument/publishDiagnostics" {
			var p PublishDiagnosticsParams
			if err := json.Unmarshal(params, &p); err == nil {
				m.diagMu.Lock()
				m.diags[p.URI] = p.Diagnostics
				m.diagMu.Unlock()
			}
		}
	})

	initParams := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   m.rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Hover: &HoverClientCapabilities{
					ContentFormat: []string{"markdown", "plaintext"},
				},
				Completion: &CompletionClientCapabilities{
					CompletionItem: &CompletionItemCapabilities{
						SnippetSupport: false,
					},
				},
				PublishDiag: &PublishDiagCapabilities{
					RelatedInformation: true,
				},
			},
		},
	}

	result, err := client.Call(ctx, "initialize", initParams)
	if err != nil {
		client.Close()
		return nil, ServerCapabilities{}, fmt.Errorf("initialize: %w", err)
	}

	var initResult InitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		client.Close()
		return nil, ServerCapabilities{}, fmt.Errorf("parse initialize result: %w", err)
	}

	// Send initialized notification.
	if err := client.Notify(ctx, "initialized", struct{}{}); err != nil {
		client.Close()
		return nil, ServerCapabilities{}, fmt.Errorf("initialized notification: %w", err)
	}

	return client, initResult.Capabilities, nil
}
