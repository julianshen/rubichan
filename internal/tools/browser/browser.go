package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
)

type OpenOptions struct {
	URL      string
	Headless bool
	Viewport Viewport
}

type Viewport struct {
	Width  int
	Height int
}

type OpenResult struct {
	URL     string
	Title   string
	Backend string
}

type ScreenshotResult struct {
	Path string
}

type WaitOptions struct {
	Selector  string
	Text      string
	TimeoutMS int
}

type Backend interface {
	Name() string
	Open(context.Context, any, OpenOptions) (any, OpenResult, error)
	Click(context.Context, any, string, bool) error
	Fill(context.Context, any, string, string, bool) error
	Snapshot(context.Context, any) (string, error)
	Screenshot(context.Context, any, string, bool, string) (ScreenshotResult, error)
	Wait(context.Context, any, WaitOptions) error
	Close(context.Context, any) error
}

type session struct {
	id      string
	backend string
	handle  any
	mu      sync.Mutex
	closed  bool
}

type Service struct {
	workDir     string
	artifactDir string
	backends    map[string]Backend
	order       []string

	mu       sync.Mutex
	sessions map[string]*session
}

type openInput struct {
	URL       string    `json:"url"`
	SessionID string    `json:"session_id,omitempty"`
	Headless  *bool     `json:"headless,omitempty"`
	Viewport  *Viewport `json:"viewport,omitempty"`
}

type clickInput struct {
	SessionID         string `json:"session_id"`
	Selector          string `json:"selector"`
	WaitForNavigation bool   `json:"wait_for_navigation,omitempty"`
}

type fillInput struct {
	SessionID string `json:"session_id"`
	Selector  string `json:"selector"`
	Value     string `json:"value"`
	Submit    bool   `json:"submit,omitempty"`
}

type snapshotInput struct {
	SessionID string `json:"session_id"`
}

type screenshotInput struct {
	SessionID string `json:"session_id"`
	Selector  string `json:"selector,omitempty"`
	FullPage  bool   `json:"full_page,omitempty"`
}

type waitInput struct {
	SessionID string `json:"session_id"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type closeInput struct {
	SessionID string `json:"session_id"`
}

func NewService(workDir string, browserCfg config.BrowserConfig, mcpServers []config.MCPServerConfig) (*Service, error) {
	artifactDir := browserCfg.ArtifactDir
	if artifactDir == "" {
		artifactDir = filepath.Join(workDir, ".rubichan", "browser")
	} else if !filepath.IsAbs(artifactDir) {
		artifactDir = filepath.Join(workDir, artifactDir)
	}
	artifactDir = filepath.Clean(artifactDir)
	if !strings.HasPrefix(artifactDir, workDir+string(filepath.Separator)) && artifactDir != workDir {
		return nil, fmt.Errorf("browser artifact_dir must stay within the workspace")
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, fmt.Errorf("create browser artifact dir: %w", err)
	}

	svc := &Service{
		workDir:     workDir,
		artifactDir: artifactDir,
		backends:    make(map[string]Backend),
		sessions:    make(map[string]*session),
	}

	native := NewNativeBackend()
	svc.backends[native.Name()] = native
	if backend, err := NewMCPBackend(workDir, browserCfg, mcpServers); err == nil && backend != nil {
		svc.backends[backend.Name()] = backend
	}

	switch browserCfg.PreferredBackend {
	case "native":
		svc.order = []string{"native", "mcp"}
	default:
		svc.order = []string{"mcp", "native"}
	}
	return svc, nil
}

func (s *Service) Open(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in openInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.URL == "" {
		return errResult("url is required"), nil
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return errResult("invalid url: %s", err), nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errResult("only http and https URLs are allowed"), nil
	}
	if err := validateBrowserTarget(u); err != nil {
		return errResult("%s", err), nil
	}

	opts := OpenOptions{URL: in.URL, Headless: true, Viewport: Viewport{Width: 1280, Height: 800}}
	if in.Headless != nil {
		opts.Headless = *in.Headless
	}
	if in.Viewport != nil {
		opts.Viewport = *in.Viewport
	}

	if in.SessionID != "" {
		existing, backend, err := s.session(in.SessionID)
		if err == nil {
			existing.mu.Lock()
			defer existing.mu.Unlock()
			if existing.closed {
				return errResult("unknown session_id %q", in.SessionID), nil
			}
			handle, result, err := backend.Open(ctx, existing.handle, opts)
			if err != nil {
				return errResult("browser_open failed: %s", err), nil
			}
			existing.handle = handle
			return tools.ToolResult{Content: fmt.Sprintf("session_id: %s\nbackend: %s\ntitle: %s\nurl: %s", existing.id, result.Backend, result.Title, result.URL)}, nil
		}
	}

	id := in.SessionID
	if id == "" {
		id = uuid.NewString()[:8]
	}
	backend := s.pickBackend()
	if backend == nil {
		return errResult("no browser backend is available"), nil
	}
	handle, result, err := backend.Open(ctx, nil, opts)
	if err != nil {
		return errResult("browser_open failed: %s", err), nil
	}
	s.mu.Lock()
	s.sessions[id] = &session{id: id, backend: backend.Name(), handle: handle}
	s.mu.Unlock()
	return tools.ToolResult{Content: fmt.Sprintf("session_id: %s\nbackend: %s\ntitle: %s\nurl: %s", id, result.Backend, result.Title, result.URL)}, nil
}

func (s *Service) Click(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in clickInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	sess, backend, err := s.session(in.SessionID)
	if err != nil {
		return errResult("%s", err), nil
	}
	if in.Selector == "" {
		return errResult("selector is required"), nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	if err := backend.Click(ctx, sess.handle, in.Selector, in.WaitForNavigation); err != nil {
		return errResult("browser_click failed: %s", err), nil
	}
	return tools.ToolResult{Content: fmt.Sprintf("clicked %q in session %s", in.Selector, in.SessionID)}, nil
}

func (s *Service) Fill(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in fillInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	sess, backend, err := s.session(in.SessionID)
	if err != nil {
		return errResult("%s", err), nil
	}
	if in.Selector == "" {
		return errResult("selector is required"), nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	if err := backend.Fill(ctx, sess.handle, in.Selector, in.Value, in.Submit); err != nil {
		return errResult("browser_fill failed: %s", err), nil
	}
	return tools.ToolResult{Content: fmt.Sprintf("filled %q in session %s", in.Selector, in.SessionID)}, nil
}

func (s *Service) Snapshot(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in snapshotInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	sess, backend, err := s.session(in.SessionID)
	if err != nil {
		return errResult("%s", err), nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	snapshot, err := backend.Snapshot(ctx, sess.handle)
	if err != nil {
		return errResult("browser_snapshot failed: %s", err), nil
	}
	return tools.ToolResult{Content: snapshot}, nil
}

func (s *Service) Screenshot(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in screenshotInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	sess, backend, err := s.session(in.SessionID)
	if err != nil {
		return errResult("%s", err), nil
	}
	filename := s.screenshotPath(in.SessionID)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	res, err := backend.Screenshot(ctx, sess.handle, in.Selector, in.FullPage, filename)
	if err != nil {
		return errResult("browser_screenshot failed: %s", err), nil
	}
	return tools.ToolResult{Content: fmt.Sprintf("saved screenshot to %s", res.Path)}, nil
}

func (s *Service) Wait(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in waitInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	sess, backend, err := s.session(in.SessionID)
	if err != nil {
		return errResult("%s", err), nil
	}
	if in.Selector == "" && in.Text == "" && in.TimeoutMS <= 0 {
		return errResult("one of selector, text, or timeout_ms is required"), nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	if err := backend.Wait(ctx, sess.handle, WaitOptions{Selector: in.Selector, Text: in.Text, TimeoutMS: in.TimeoutMS}); err != nil {
		return errResult("browser_wait failed: %s", err), nil
	}
	return tools.ToolResult{Content: fmt.Sprintf("wait completed for session %s", in.SessionID)}, nil
}

func (s *Service) Close(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in closeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	s.mu.Lock()
	sess := s.sessions[in.SessionID]
	if sess != nil {
		delete(s.sessions, in.SessionID)
	}
	s.mu.Unlock()
	if sess == nil {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	backend := s.backends[sess.backend]
	if backend == nil {
		return errResult("backend %q is unavailable", sess.backend), nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errResult("unknown session_id %q", in.SessionID), nil
	}
	sess.closed = true
	if err := backend.Close(ctx, sess.handle); err != nil {
		return errResult("browser_close failed: %s", err), nil
	}
	sess.handle = nil
	return tools.ToolResult{Content: fmt.Sprintf("closed session %s", in.SessionID)}, nil
}

func (s *Service) session(id string) (*session, Backend, error) {
	if id == "" {
		return nil, nil, fmt.Errorf("session_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return nil, nil, fmt.Errorf("unknown session_id %q", id)
	}
	backend := s.backends[sess.backend]
	if backend == nil {
		return nil, nil, fmt.Errorf("backend %q is unavailable", sess.backend)
	}
	return sess, backend, nil
}

func (s *Service) pickBackend() Backend {
	for _, name := range s.order {
		if backend := s.backends[name]; backend != nil {
			return backend
		}
	}
	return nil
}

func (s *Service) screenshotPath(sessionID string) string {
	clean := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, sessionID)
	filename := fmt.Sprintf("%s-%d.png", clean, time.Now().UnixNano())
	return filepath.Join(s.artifactDir, filename)
}

// validateBrowserTarget resolves the URL's host and rejects private/local IPs
// to prevent SSRF attacks via the browser tool.
func validateBrowserTarget(u *url.URL) error {
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	for _, addr := range addrs {
		if isPrivateAddress(addr.IP) {
			return fmt.Errorf("requests to private or local addresses are not allowed")
		}
	}
	return nil
}

func isPrivateAddress(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func errResult(format string, args ...any) tools.ToolResult {
	return tools.ToolResult{Content: fmt.Sprintf(format, args...), IsError: true}
}

type tool struct {
	name        string
	description string
	schema      json.RawMessage
	run         func(context.Context, json.RawMessage) (tools.ToolResult, error)
}

func (t *tool) Name() string                 { return t.name }
func (t *tool) Description() string          { return t.description }
func (t *tool) InputSchema() json.RawMessage { return t.schema }
func (t *tool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	return t.run(ctx, input)
}

func NewTools(service *Service) []tools.Tool {
	return []tools.Tool{
		&tool{
			name:        "browser_open",
			description: "Open a browser session and navigate to a URL.",
			schema:      json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"session_id":{"type":"string"},"headless":{"type":"boolean"},"viewport":{"type":"object","properties":{"width":{"type":"integer"},"height":{"type":"integer"}}}},"required":["url"]}`),
			run:         service.Open,
		},
		&tool{
			name:        "browser_click",
			description: "Click an element in a browser session using a CSS selector.",
			schema:      json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"},"selector":{"type":"string"},"wait_for_navigation":{"type":"boolean"}},"required":["session_id","selector"]}`),
			run:         service.Click,
		},
		&tool{
			name:        "browser_fill",
			description: "Fill an input element in a browser session using a CSS selector.",
			schema:      json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"},"selector":{"type":"string"},"value":{"type":"string"},"submit":{"type":"boolean"}},"required":["session_id","selector","value"]}`),
			run:         service.Fill,
		},
		&tool{
			name:        "browser_snapshot",
			description: "Capture a text snapshot of the current browser page.",
			schema:      json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"}},"required":["session_id"]}`),
			run:         service.Snapshot,
		},
		&tool{
			name:        "browser_screenshot",
			description: "Save a screenshot of the current page or a matching element.",
			schema:      json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"},"selector":{"type":"string"},"full_page":{"type":"boolean"}},"required":["session_id"]}`),
			run:         service.Screenshot,
		},
		&tool{
			name:        "browser_wait",
			description: "Wait for text, a selector, or a timeout in a browser session.",
			schema:      json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"},"selector":{"type":"string"},"text":{"type":"string"},"timeout_ms":{"type":"integer"}},"required":["session_id"]}`),
			run:         service.Wait,
		},
		&tool{
			name:        "browser_close",
			description: "Close a browser session.",
			schema:      json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"}},"required":["session_id"]}`),
			run:         service.Close,
		},
	}
}
