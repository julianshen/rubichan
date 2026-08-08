package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
)

var clientNextID int64 // shared across Clients; only uniqueness within a process matters

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

// call sends a JSON-RPC request to method with params, decodes the result
// into result (a pointer), and returns any RPC-level error as a Go error.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params for %s: %w", method, err)
		}
		rawParams = data
	}

	id := atomic.AddInt64(&clientNextID, 1)
	body, err := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: rawParams})
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request for %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer resp.Body.Close()

	var rpcResp RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("decode response for %s: %w", method, err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("%s failed: %s (code %d)", method, rpcResp.Error.Message, rpcResp.Error.Code)
	}
	if result != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("unmarshal result for %s: %w", method, err)
		}
	}
	return nil
}

// Send makes a message/send call and returns the resulting Task. If the
// task isn't terminal yet, the caller polls GetTask.
func (c *Client) Send(ctx context.Context, msg Message) (*Task, error) {
	var task Task
	if err := c.call(ctx, "message/send", struct {
		Message Message `json:"message"`
	}{Message: msg}, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTask fetches the current state of a previously created task.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	var task Task
	if err := c.call(ctx, "tasks/get", struct {
		ID string `json:"id"`
	}{ID: taskID}, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// CancelTask requests cancellation of a running task.
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	return c.call(ctx, "tasks/cancel", struct {
		ID string `json:"id"`
	}{ID: taskID}, nil)
}
