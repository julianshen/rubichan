package ws

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gobwas/ws"
)

// ServerConfig configures the WebSocket server.
type ServerConfig struct {
	Addr                string
	Auth                Authenticator
	ReconnectBufferSize int
	AgentFactory        AgentFactory
}

// Server is the WebSocket transport server.
type Server struct {
	hub        *Hub
	auth       Authenticator
	httpServer *http.Server
}

// NewServer creates a new WebSocket server.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Auth == nil {
		cfg.Auth = NoopAuth{}
	}

	hub := NewHub(HubConfig{
		ReconnectBufferSize: cfg.ReconnectBufferSize,
		AgentFactory:        cfg.AgentFactory,
	})

	mux := http.NewServeMux()

	s := &Server{
		hub:  hub,
		auth: cfg.Auth,
		httpServer: &http.Server{
			Addr:         cfg.Addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}

	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	return s
}

// Hub returns the server's hub for direct access.
func (s *Server) Hub() *Hub {
	return s.hub
}

// ListenAndServe starts the server.
func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

// Serve starts the server on the given listener.
func (s *Server) Serve(ln net.Listener) error {
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.hub.Close()
	return s.httpServer.Shutdown(ctx)
}

// handleWebSocket upgrades HTTP to WebSocket and starts client pumps.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate.
	token := TokenFromRequest(r)
	claims, err := s.auth.Validate(token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade connection.
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}

	client := newClient(s.hub, conn, claims)

	go client.writePump()
	go client.readPump()
}

// handleHealth returns a simple health check response.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok"}`)
}
