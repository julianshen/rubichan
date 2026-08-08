package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client calls a remote A2A agent over HTTP + JSON-RPC 2.0.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a Client for the agent at baseURL. apiKey, if non-empty,
// is sent as a Bearer token on every request. httpClient defaults to
// http.DefaultClient if nil.
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient, baseURL: baseURL, apiKey: apiKey}
}

// Discover fetches and parses {baseURL}/.well-known/agent.json.
func (c *Client) Discover(ctx context.Context) (*AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/.well-known/agent.json", nil)
	if err != nil {
		return nil, fmt.Errorf("build discover request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch agent card: unexpected status %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("parse agent card: %w", err)
	}
	return &card, nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
