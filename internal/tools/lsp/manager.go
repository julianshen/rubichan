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
	onError     func(error) // optional handler for non-fatal errors

	mu       sync.Mutex
	closed   bool                     // set by Shutdown; prevents new server registration
	servers  map[string]*serverHandle // language -> handle
	starting map[string]*serverInit   // language -> in-progress init

	docsMu sync.Mutex     // serializes document open/change operations
	docs   map[string]int // URI -> version (for didOpen/didChange tracking)

	diagMu sync.RWMutex
	diags  map[string][]Diagnostic // URI -> latest diagnostics
}

// serverHandle wraps a running language server.
type serverHandle struct {
	client       *Client
	capabilities ServerCapabilities
}

// serverInit tracks an in-progress server startup so concurrent callers
// can wait on the result instead of all attempting to spawn.
type serverInit struct {
	done   chan struct{}
	handle *serverHandle
	err    error
}

// NewManager creates a new manager for the given workspace root.
func NewManager(registry *Registry, rootDir string) *Manager {
	return &Manager{
		registry:    registry,
		rootURI:     pathToURI(rootDir),
		summarizer:  DefaultSummarizer(),
		spawnServer: spawnServer,
		servers:     make(map[string]*serverHandle),
		starting:    make(map[string]*serverInit),
		docs:        make(map[string]int),
		diags:       make(map[string][]Diagnostic),
	}
}

// SetSummarizer sets a custom summarizer for response truncation.
// Safe for concurrent use with tool execution.
func (m *Manager) SetSummarizer(s *Summarizer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summarizer = s
}

// getSummarizer returns the current summarizer under the lock.
func (m *Manager) getSummarizer() *Summarizer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.summarizer
}

// ServerFor returns the client for the given language, starting the server if
// needed. Returns ErrServerNotInstalled if the binary is not on PATH, or
// ErrManagerShutdown if Shutdown has been called.
// The global mu is released during the blocking startServer call so
// concurrent callers for other languages are not blocked.
func (m *Manager) ServerFor(ctx context.Context, languageID string) (*Client, *ServerCapabilities, error) {
	m.mu.Lock()

	// Reject new servers after Shutdown has been called.
	if m.closed {
		m.mu.Unlock()
		return nil, nil, ErrManagerShutdown
	}

	// Fast path: server already running.
	if handle, ok := m.servers[languageID]; ok {
		m.mu.Unlock()
		return handle.client, &handle.capabilities, nil
	}

	// Another goroutine is already starting this server — wait for it.
	if init, ok := m.starting[languageID]; ok {
		m.mu.Unlock()
		select {
		case <-init.done:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
		if init.err != nil {
			return nil, nil, init.err
		}
		return init.handle.client, &init.handle.capabilities, nil
	}

	cfg, err := m.registry.ConfigFor(languageID)
	if err != nil {
		m.mu.Unlock()
		return nil, nil, err
	}

	if !m.registry.IsInstalled(languageID) {
		m.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: %s (%s not found on PATH)", ErrServerNotInstalled, languageID, cfg.Command)
	}

	// Claim this language's init and release mu during the blocking spawn.
	init := &serverInit{done: make(chan struct{})}
	m.starting[languageID] = init
	m.mu.Unlock()

	// Ensure waiters are always unblocked, even if startServer panics.
	// On panic, init.err will have been set and starting entry cleaned up.
	defer close(init.done)

	var startErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				startErr = fmt.Errorf("server startup panicked: %v", r)
			}
		}()
		var client *Client
		var caps ServerCapabilities
		client, caps, startErr = m.startServer(ctx, cfg)
		if startErr == nil {
			init.handle = &serverHandle{client: client, capabilities: caps}
		}
	}()

	m.mu.Lock()
	delete(m.starting, languageID)
	if startErr != nil {
		init.err = fmt.Errorf("start %s server: %w", languageID, startErr)
		m.mu.Unlock()
		return nil, nil, init.err
	}
	// If Shutdown ran while we were starting, close the newly-created server
	// instead of publishing it — otherwise it becomes an orphaned process.
	if m.closed {
		m.mu.Unlock()
		if init.handle != nil && init.handle.client != nil {
			init.handle.client.Close()
		}
		init.err = ErrManagerShutdown
		return nil, nil, ErrManagerShutdown
	}
	if init.handle != nil {
		m.servers[languageID] = init.handle
	}
	m.mu.Unlock()

	return init.handle.client, &init.handle.capabilities, nil
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

	// Hold docsMu for the entire operation to prevent races with EnsureFileOpen.
	m.docsMu.Lock()
	defer m.docsMu.Unlock()

	version, opened := m.docs[uri]
	version++

	if !opened {
		// First time seeing this file — send didOpen.
		err = client.Notify(ctx, "textDocument/didOpen", DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        uri,
				LanguageID: lang,
				Version:    version,
				Text:       string(content),
			},
		})
	} else {
		// File already open — send didChange with full content.
		err = client.Notify(ctx, "textDocument/didChange", DidChangeTextDocumentParams{
			TextDocument: VersionedTextDocumentIdentifier{
				URI:     uri,
				Version: version,
			},
			ContentChanges: []TextDocumentContentChangeEvent{
				{Text: string(content)},
			},
		})
	}

	if err != nil {
		return err
	}
	m.docs[uri] = version
	return nil
}

// Shutdown gracefully stops all running servers by sending the LSP shutdown
// request followed by the exit notification, then closing the transport.
// It also waits for any in-progress server starts to complete before
// shutting them down, preventing orphaned server processes.
func (m *Manager) Shutdown(ctx context.Context) error {
	// Mark the manager as closed so that any in-flight or future ServerFor
	// calls will not publish new handles into m.servers after we clear it.
	m.mu.Lock()
	m.closed = true
	pending := make([]*serverInit, 0, len(m.starting))
	for _, init := range m.starting {
		pending = append(pending, init)
	}
	m.mu.Unlock()

	for _, init := range pending {
		select {
		case <-init.done:
		case <-ctx.Done():
			// Best-effort: proceed with what we have.
		}
	}

	// Snapshot the server map and clear it under the lock, then release
	// before doing blocking network I/O (shutdown/exit/close).
	m.mu.Lock()
	snapshot := m.servers
	m.servers = make(map[string]*serverHandle)
	m.mu.Unlock()

	var errs []error
	for lang, handle := range snapshot {
		if handle.client != nil {
			_, _ = handle.client.Call(ctx, "shutdown", nil)
			_ = handle.client.Notify(ctx, "exit", nil)
			if err := handle.client.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close %s: %w", lang, err))
			}
		}
	}
	return errors.Join(errs...)
}

// EnsureFileOpen sends textDocument/didOpen if the file hasn't been opened yet.
// Must be called before position-based LSP requests to ensure the server knows
// about the file. Returns an error if the file cannot be read or the notification fails.
func (m *Manager) EnsureFileOpen(ctx context.Context, client *Client, filePath string) error {
	uri := pathToURI(filePath)

	// Hold docsMu for the entire open sequence to prevent races between
	// EnsureFileOpen and NotifyFileChanged for the same file.
	m.docsMu.Lock()
	defer m.docsMu.Unlock()

	if _, opened := m.docs[uri]; opened {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
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
		return fmt.Errorf("didOpen notification: %w", err)
	}

	m.docs[uri] = 1
	return nil
}

// startServer spawns a language server process and performs the initialize handshake.
func (m *Manager) startServer(ctx context.Context, cfg ServerConfig) (*Client, ServerCapabilities, error) {
	proc, err := m.spawnServer(cfg)
	if err != nil {
		return nil, ServerCapabilities{}, fmt.Errorf("spawn: %w", err)
	}

	errHandler := func(err error) {
		if m.onError != nil {
			m.onError(err)
		}
	}

	client := newClient(proc, func(method string, params json.RawMessage) {
		if method == "textDocument/publishDiagnostics" {
			var p PublishDiagnosticsParams
			if err := json.Unmarshal(params, &p); err != nil {
				errHandler(fmt.Errorf("publishDiagnostics unmarshal: %w", err))
				return
			}
			m.diagMu.Lock()
			m.diags[p.URI] = p.Diagnostics
			m.diagMu.Unlock()
		}
	}, errHandler)

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
