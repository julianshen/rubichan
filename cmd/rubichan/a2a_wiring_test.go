package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/testutil"
)

func TestBuildA2ARegistryPopulatesFromConfig(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"weather-bot","skills":[{"id":"forecast","tags":["weather"]}]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "weather-bot", URL: srv.URL}}

	reg := buildA2ARegistry(context.Background(), cfg)
	assert.NotEmpty(t, reg.Match("weather"))
}

func TestBuildA2ARegistryToleratesUnreachableAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "gone", URL: "mem://does-not-exist"}}

	reg := buildA2ARegistry(context.Background(), cfg)
	assert.Empty(t, reg.Match("anything"), "an unreachable agent must not appear in the registry, and must not error out registry construction")
}

// TestBuildA2ARegistryResolvesEnvSourcedAPIKey proves that an
// [[a2a.agents]] entry with api_key_source = "env" actually resolves its
// key from the environment and sends it as the Authorization header on
// Discover. Before the fix, buildA2ARegistry passed "" as the env var name
// to config.ResolveAPIKey, so the "env" branch always failed and the
// request went out unauthenticated; the handler below rejects any request
// missing the expected Bearer token, so the agent would silently never
// appear in the registry.
func TestBuildA2ARegistryResolvesEnvSourcedAPIKey(t *testing.T) {
	const agentName = "research-bot"
	const wantKey = "sekret-env-key"
	envVar := config.OpenAICompatibleEnvVar(agentName)
	t.Setenv(envVar, wantKey)

	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"research-bot","skills":[{"id":"research","tags":["research"]}]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{
		Name:         agentName,
		URL:          srv.URL,
		APIKeySource: "env",
	}}

	reg := buildA2ARegistry(context.Background(), cfg)
	assert.NotEmpty(t, reg.Match("research"), "agent with env-sourced api key must resolve its key and appear in the registry")
}

// TestBuildA2ARegistryBoundsPerAgentDiscoveryTimeout proves that a single
// configured agent that never responds (e.g. a firewalled or stale host)
// cannot hang buildA2ARegistry indefinitely. Before the fix, Discover was
// called with an undeadlined context and a nil *http.Client (defaulting to
// http.DefaultClient, itself timeout-less), so a stalled handler would
// block forever.
func TestBuildA2ARegistryBoundsPerAgentDiscoveryTimeout(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		// Simulate a host that accepts the connection but never responds.
		<-r.Context().Done()
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "stalled", URL: srv.URL}}

	done := make(chan *a2a.Registry, 1)
	go func() {
		done <- buildA2ARegistry(context.Background(), cfg)
	}()

	select {
	case reg := <-done:
		assert.Empty(t, reg.Match("anything"), "a stalled agent must not appear in the registry")
	case <-time.After(8 * time.Second):
		t.Fatal("buildA2ARegistry did not return within the bounded timeout; a stalled agent hung startup")
	}
}
