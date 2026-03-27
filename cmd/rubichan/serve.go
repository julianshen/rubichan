package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	ws "github.com/julianshen/rubichan/internal/transport/ws"
)

var (
	serveAddr  string
	serveToken string
)

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the WebSocket server for remote agent connections",
		Long: `Start a WebSocket server that exposes the agent core over WebSocket.

Clients (web UIs, TUIs, event bridges) connect to the /ws endpoint and
exchange JSON-encoded envelopes to create sessions, send messages,
receive streaming events, and handle approval flows.

Endpoints:
  GET /ws      WebSocket upgrade (pass token via ?token= or Authorization header)
  GET /health  Health check`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServe()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1:8080", "listen address (host:port)")
	cmd.Flags().StringVar(&serveToken, "token", "", "static auth token (empty = no auth)")

	return cmd
}

func runServe() error {
	var auth ws.Authenticator
	if serveToken != "" {
		var err error
		auth, err = ws.NewStaticTokenAuth(serveToken)
		if err != nil {
			return fmt.Errorf("configure auth: %w", err)
		}
	} else {
		auth = ws.NoopAuth{}
	}

	srv := ws.NewServer(ws.ServerConfig{
		Addr: serveAddr,
		Auth: auth,
	})

	ln, err := net.Listen("tcp", serveAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", serveAddr, err)
	}

	log.Printf("rubichan serve: listening on %s", ln.Addr())
	if serveToken == "" {
		log.Print("rubichan serve: WARNING: no auth token set — all connections accepted")
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Print("rubichan serve: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
