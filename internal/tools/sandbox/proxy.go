package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// ProxyOption configures a DomainProxy.
type ProxyOption func(*DomainProxy)

// WithOnBlocked sets a callback invoked when a request is blocked.
// The callback receives the blocked domain and a description of the command
// (e.g. "GET http://evil.com/path" or "CONNECT evil.com:443").
func WithOnBlocked(fn func(domain, command string)) ProxyOption {
	return func(p *DomainProxy) {
		p.onBlocked = fn
	}
}

// DomainProxy is an HTTP proxy that filters requests by domain allowlist.
// It handles both plain HTTP requests and HTTPS CONNECT tunneling.
type DomainProxy struct {
	listener  net.Listener
	allowed   []string // immutable after construction
	onBlocked func(string, string)
	mu        sync.RWMutex
	runtime   map[string]bool // session-only additions
	server    *http.Server
}

// NewDomainProxy creates a new domain-filtering proxy.
// The allowed list is immutable after construction; use AllowDomain for
// runtime additions.
func NewDomainProxy(allowed []string, opts ...ProxyOption) *DomainProxy {
	p := &DomainProxy{
		allowed: allowed,
		runtime: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Start begins listening on a random port and serving requests.
// It returns the listener address (e.g. "127.0.0.1:12345").
func (p *DomainProxy) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("proxy listen: %w", err)
	}
	p.listener = ln
	p.server = &http.Server{Handler: p}

	go p.server.Serve(ln)

	return ln.Addr().String(), nil
}

// Stop gracefully shuts down the proxy. It is safe to call multiple times.
func (p *DomainProxy) Stop() error {
	if p.server == nil {
		return nil
	}
	err := p.server.Shutdown(context.Background())
	p.server = nil
	return err
}

// Port returns the port the proxy is listening on.
func (p *DomainProxy) Port() int {
	if p.listener == nil {
		return 0
	}
	return p.listener.Addr().(*net.TCPAddr).Port
}

// AllowDomain adds a domain to the runtime allowlist (session-only, not persisted).
func (p *DomainProxy) AllowDomain(domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runtime[strings.ToLower(domain)] = true
}

// isAllowed checks whether a domain is permitted by the static or runtime allowlist.
func (p *DomainProxy) isAllowed(domain string) bool {
	domain = strings.ToLower(domain)

	// Check runtime allowlist first.
	p.mu.RLock()
	if p.runtime[domain] {
		p.mu.RUnlock()
		return true
	}
	p.mu.RUnlock()

	// Check static allowlist.
	return MatchDomain(domain, p.allowed)
}

// hostWithoutPort strips the port from a host:port string.
func hostWithoutPort(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host // no port present
	}
	return h
}

// ServeHTTP dispatches HTTP requests and CONNECT tunnels.
func (p *DomainProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func (p *DomainProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	domain := hostWithoutPort(r.Host)
	if !p.isAllowed(domain) {
		command := fmt.Sprintf("%s %s", r.Method, r.URL.String())
		if p.onBlocked != nil {
			p.onBlocked(domain, command)
		}
		http.Error(w, "domain blocked by proxy", http.StatusForbidden)
		return
	}

	// Clear RequestURI — Go's HTTP client rejects outgoing requests with it set.
	r.RequestURI = ""

	// Strip hop-by-hop headers.
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("Proxy-Connection")

	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (p *DomainProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	domain := hostWithoutPort(r.Host)
	if !p.isAllowed(domain) {
		command := fmt.Sprintf("CONNECT %s", r.Host)
		if p.onBlocked != nil {
			p.onBlocked(domain, command)
		}
		http.Error(w, "domain blocked by proxy", http.StatusForbidden)
		return
	}

	// Dial the target.
	targetConn, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy connect error: %v", err), http.StatusBadGateway)
		return
	}

	// Hijack the client connection.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		targetConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		targetConn.Close()
		http.Error(w, fmt.Sprintf("hijack error: %v", err), http.StatusInternalServerError)
		return
	}

	// Send 200 Connection Established.
	clientBuf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	clientBuf.Flush()

	// Bidirectional copy.
	go func() {
		io.Copy(targetConn, clientConn)
		targetConn.Close()
	}()
	go func() {
		io.Copy(clientConn, targetConn)
		clientConn.Close()
	}()
}
