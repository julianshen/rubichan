package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
)

var (
	a2aServeAddr   string
	a2aServeNoAuth bool
)

// buildA2ARegistry discovers every configured [[a2a.agents]] entry and
// populates a Registry. An agent that fails to respond is logged and
// skipped, not fatal — a2a_dispatch simply won't route to it.
func buildA2ARegistry(ctx context.Context, cfg *config.Config) *a2a.Registry {
	reg := a2a.NewRegistry()
	for _, agentCfg := range cfg.A2A.Agents {
		apiKey, _ := config.ResolveAPIKey(agentCfg.APIKeySource, agentCfg.APIKey, config.OpenAICompatibleEnvVar(agentCfg.Name))
		client := a2a.NewClient(agentCfg.URL, apiKey, nil)
		card, err := discoverWithTimeout(ctx, client, agentDiscoverTimeout)
		if err != nil {
			log.Printf("a2a: could not discover configured agent %q: %v", agentCfg.Name, err)
			continue
		}
		reg.Add(agentCfg.Name, *card)
	}
	return reg
}

// agentDiscoverTimeout bounds each configured agent's discovery attempt
// during startup so one slow or unreachable agent (firewalled, stale
// address, VPN-only) cannot hang the CLI's startup indefinitely.
const agentDiscoverTimeout = 5 * time.Second

func discoverWithTimeout(ctx context.Context, client *a2a.Client, timeout time.Duration) (*a2a.AgentCard, error) {
	discoverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.Discover(discoverCtx)
}

func a2aCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "a2a",
		Short: "A2A (Agent2Agent) protocol commands",
	}
	cmd.AddCommand(a2aServeCmd())
	return cmd
}

func a2aServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Expose rubichan as a callable A2A agent",
		Long: `Start an HTTP server implementing the A2A (Agent2Agent) protocol,
so other agents can discover and call rubichan.

Endpoints:
  GET  /.well-known/agent.json   Agent Card
  POST /                         JSON-RPC 2.0 (message/send, message/stream, tasks/get, tasks/cancel)

This server is network-facing: any authenticated caller can invoke every
enabled tool. Use --tools to restrict which tools are exposed to remote
callers (defaults to all tools, matching interactive mode).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runA2AServe(cmd)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().StringVar(&a2aServeAddr, "addr", "127.0.0.1:9910", "listen address (host:port)")
	cmd.Flags().BoolVar(&a2aServeNoAuth, "no-auth", false, "disable auth (refuses to bind on a non-loopback address unless set)")
	return cmd
}

// resolveA2AServeAddr decides which listen address to use: the --addr flag
// wins only when the user explicitly passed it; otherwise config.toml's
// a2a.serve_addr (or its own default) is left in place. Extracted so the
// precedence logic is unit-testable without starting a real server.
func resolveA2AServeAddr(addrChanged bool, flagAddr, cfgAddr string) string {
	if addrChanged {
		return flagAddr
	}
	return cfgAddr
}

// addressAllowsNoAuth reports whether ip is a loopback address, the only
// class runA2AServe will bind to with auth disabled. Callers must check this
// against the listener's actual resolved address (ln.Addr()), not the
// pre-resolution host text — a hosts-file or DNS misconfiguration can make a
// text check like `host == "localhost"` pass while the real bound address
// isn't loopback at all. Extracted so this decision is unit-testable without
// starting a real listener.
func addressAllowsNoAuth(ip net.IP) bool {
	return ip.IsLoopback()
}

// advertisedCardURL builds the Agent Card's base URL from the listener's
// actual resolved address. Building it from the pre-listen config string
// instead would advertise an unreachable address whenever the configured
// port is 0 (OS-assigned), since the real port is only known after
// net.Listen succeeds. Extracted so this decision is unit-testable without
// starting a real listener.
func advertisedCardURL(addr net.Addr) string {
	return "http://" + addr.String()
}

func runA2AServe(cmd *cobra.Command) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Only let the flag override config.toml's a2a.serve_addr when the user
	// actually passed --addr; the flag's non-empty default would otherwise
	// silently clobber the config value on every run.
	cfg.A2A.ServeAddr = resolveA2AServeAddr(cmd.Flags().Changed("addr"), a2aServeAddr, cfg.A2A.ServeAddr)
	requireAuth := cfg.A2A.RequireAuth && !a2aServeNoAuth

	apiKey, err := config.ResolveAPIKey(cfg.A2A.APIKeySource, cfg.A2A.APIKey, "RUBICHAN_A2A_API_KEY")
	if requireAuth && err != nil {
		return fmt.Errorf("resolve a2a api key: %w", err)
	}

	llm, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	registry := tools.NewRegistry()
	// This server is network-facing: without --tools, every core tool (including
	// shell and file write) is exposed to any authenticated caller. Operators
	// serving untrusted callers should pass --tools to restrict exposure.
	toolsCfg := ToolsConfig{CLIOverrides: parseToolsFlag(toolsFlag)}
	if _, err := registerCoreTools(".", registry, cfg, toolsCfg, tools.NewDiffTracker(), 120*time.Second); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}

	approve := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	a := agent.New(llm, registry, approve, cfg)

	cfgDir, err := configDir()
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	st, err := openStore(cfgDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ln, err := net.Listen("tcp", cfg.A2A.ServeAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.A2A.ServeAddr, err)
	}

	// Gate --no-auth against the listener's actual resolved address, not the
	// pre-resolution host text: a misconfigured resolver (e.g. /etc/hosts
	// mapping "localhost" to a non-loopback IP) could otherwise pass a text
	// check while binding to an address reachable off-host.
	if !requireAuth {
		tcpAddr, ok := ln.Addr().(*net.TCPAddr)
		if !ok || !addressAllowsNoAuth(tcpAddr.IP) {
			ln.Close()
			return fmt.Errorf("refusing to bind %s without auth; pass an api key or use --addr on loopback only", cfg.A2A.ServeAddr)
		}
	}

	// Built from the listener's actual bound address, not the pre-listen
	// config string, so the card advertises a connectable URL even when
	// --addr requested an OS-assigned port (e.g. 127.0.0.1:0).
	card := a2a.BuildAgentCard(registry, "rubichan", "A terminal-first AI coding agent", advertisedCardURL(ln.Addr()))
	srv := a2a.NewServer(a, a2a.NewStoreTaskStore(st, "server"), card, requireAuth, apiKey)

	log.Printf("rubichan a2a serve: listening on %s", ln.Addr())
	if !requireAuth {
		log.Print("rubichan a2a serve: WARNING: auth disabled")
	}

	httpSrv := &http.Server{
		Handler: srv.Handler(),
		// Bounds how long the server waits for request headers to arrive,
		// closing the Slowloris-style hole of a client that opens a
		// connection and trickles headers forever. Deliberately not a
		// blanket ReadTimeout (which bounds the whole request, body
		// included): this server needs to allow long-running agent turns
		// and SSE streams once headers are in. Matches the precedent in
		// internal/transport/ws/server.go's ReadTimeout value.
		ReadHeaderTimeout: 15 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Print("rubichan a2a serve: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
