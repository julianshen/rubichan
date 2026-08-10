package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

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
