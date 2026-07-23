// Command embed is a runnable demonstration of embedding the rubichan
// agent core in another program and opting into exactly the modules you
// want — the payoff of the modular-core redesign (docs/MODULAR_CORE_REDESIGN.md).
//
// It composes agent.New with three of the seams the redesign introduced,
// each a small interface in pkg/agentsdk:
//
//   - ContextStrategy    — contributes a section to every system prompt
//   - BackgroundTask     — runs work concurrently with the agent loop
//   - tool middleware    — wraps every tool execution
//
// No TUI, no headless runner, no ACP: just the core loop plus the modules
// this embedder chose. Adding a capability adds a module, not a core
// struct field.
//
// The example is self-contained — it uses a tiny canned provider so it
// runs with no API key. In a real embedder you would pass a provider
// built from internal/provider (Anthropic, OpenAI, Ollama, …) instead.
//
// Reachability note: the registration options (WithContextStrategies,
// WithBackgroundTasks, WithToolMiddlewares) currently live on
// internal/agent, so an embedder must be inside this module to call them
// — as this example is. The interfaces they take (agentsdk.ContextStrategy,
// agentsdk.BackgroundTask, agentsdk.Middleware) are already public and
// stable in pkg/agentsdk; promoting the core constructor to pkg/ is the
// remaining step that would let a different module embed it too.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// sessionMemoryMaxTokens is the MaxTokens the built-in session-memory
// extraction call uses (see internal/agent/session_memory_service.go). The
// scripted provider routes those calls to a trivial reply so the async
// extraction — registered automatically by agent.New — never disturbs the
// two-response demo script or races on its counter.
const sessionMemoryMaxTokens = 16384

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "embed example failed:", err)
		os.Exit(1)
	}
}

// embedder bundles the composed agent with handles to the modules it was
// given, so both main and the integration test observe the same wiring.
type embedder struct {
	agent     *agent.Agent
	provider  *scriptedProvider
	strategy  *deploymentWindowStrategy
	auditor   *sessionAuditor
	toolCalls *callCounter
}

// compose wires the core with three modules. This is the whole point of
// the example: agent.New plus a few Use-style options, no bespoke struct.
func compose() embedder {
	// A tool the demo turn will call, so the middleware and the
	// background-task join both have something to observe.
	registry := tools.NewRegistry()
	_ = registry.Register(greetTool{})

	// Module 1 — a ContextStrategy that injects a section into every
	// system prompt. A real one might pull runbook links, on-call info,
	// or feature-flag state.
	deployWindow := &deploymentWindowStrategy{}

	// Module 2 — a BackgroundTask that observes the loop lifecycle.
	auditor := &sessionAuditor{}

	// Module 3 — a tool-execution middleware that counts calls. A real
	// one might enforce policy, add tracing, or redact output.
	toolCalls := &callCounter{}
	countingMW := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			toolCalls.inc()
			return next(ctx, tc)
		}
	}

	provider := cannedProvider()
	agentCore := agent.New(
		provider,
		registry,
		autoApprove,
		config.DefaultConfig(),
		agent.WithContextStrategies(deployWindow),
		agent.WithBackgroundTasks(auditor),
		agent.WithToolMiddlewares(agent.ToolMiddlewares{
			BeforeHooks: []toolexec.Middleware{countingMW},
		}),
	)

	return embedder{
		agent:     agentCore,
		provider:  provider,
		strategy:  deployWindow,
		auditor:   auditor,
		toolCalls: toolCalls,
	}
}

// run composes the embedder, drives one turn, and reports what the modules
// observed.
func run(out interface{ Write([]byte) (int, error) }) error {
	e := compose()

	assistant, err := e.driveTurn(context.Background(), "greet the release team")
	if err != nil {
		return err
	}

	// EndSession fires on its own goroutine after the loop exits, so the
	// turn channel closing does not guarantee "end" has been recorded yet.
	// Wait for it so the summary shows the full start/join/end lifecycle
	// the example is meant to demonstrate.
	e.auditor.waitForEnd(2 * time.Second)

	fmt.Fprintf(out, "assistant said: %s\n", assistant)
	fmt.Fprintf(out, "tool middleware saw %d call(s)\n", e.toolCalls.get())
	fmt.Fprintf(out, "background task observed: %v\n", e.auditor.events())
	fmt.Fprintf(out, "context strategy contributed %d time(s)\n", e.strategy.calls())
	return nil
}

// driveTurn runs one turn to completion and returns the assistant text.
func (e embedder) driveTurn(ctx context.Context, msg string) (string, error) {
	ch, err := e.agent.Turn(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("turn: %w", err)
	}
	var assistant string
	for ev := range ch {
		switch ev.Type {
		case "text_delta":
			assistant += ev.Text
		case "error":
			if ev.Error != nil {
				return "", fmt.Errorf("turn error: %w", ev.Error)
			}
		}
	}
	return assistant, nil
}

// callCounter is a mutex-guarded counter — tool middleware may run off the
// turn goroutine when tools execute in parallel.
type callCounter struct {
	mu sync.Mutex
	n  int
}

func (c *callCounter) inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *callCounter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// autoApprove approves every tool call — fine for a non-interactive demo.
func autoApprove(context.Context, string, json.RawMessage) (bool, error) {
	return true, nil
}

// deploymentWindowStrategy is a ContextStrategy: it contributes one
// system-prompt section per turn.
type deploymentWindowStrategy struct{ n int }

func (s *deploymentWindowStrategy) ContributePromptSections(_ context.Context, _ agentsdk.PromptContext) []agentsdk.PromptSection {
	s.n++
	return []agentsdk.PromptSection{{
		Title:   "Deployment Window",
		Content: "Production deploys are frozen this week; propose changes but do not apply them.",
		Reason:  "operational state varies per run",
	}}
}

func (s *deploymentWindowStrategy) calls() int { return s.n }

// sessionAuditor is a BackgroundTask: started before each model call,
// joined after tool execution, and signalled once at session end. Its log
// is mutex-guarded because EndSession runs on its own goroutine, off the
// turn's critical path.
type sessionAuditor struct {
	mu  sync.Mutex
	log []string
}

func (a *sessionAuditor) record(s string) {
	a.mu.Lock()
	a.log = append(a.log, s)
	a.mu.Unlock()
}

func (a *sessionAuditor) StartTurn(context.Context, agentsdk.BackgroundTurnInfo) func(context.Context) {
	a.record("start")
	return func(context.Context) { a.record("join") }
}

func (a *sessionAuditor) EndSession(context.Context) { a.record("end") }

func (a *sessionAuditor) events() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.log...)
}

// waitForEnd blocks until EndSession has been recorded or timeout elapses,
// so callers can report the completed lifecycle.
func (a *sessionAuditor) waitForEnd(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		ended := len(a.log) > 0 && a.log[len(a.log)-1] == "end"
		a.mu.Unlock()
		if ended {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// scriptedProvider is a canned LLMProvider so the example runs with no
// API key: the first foreground call asks for the greet tool, the second
// replies with text. It records the system prompts it received so the
// integration test can confirm the ContextStrategy's section arrived.
type scriptedProvider struct {
	mu         sync.Mutex
	foreground int
	systems    []string
}

func cannedProvider() *scriptedProvider { return &scriptedProvider{} }

func (p *scriptedProvider) Stream(_ context.Context, req agentsdk.CompletionRequest) (<-chan agentsdk.StreamEvent, error) {
	p.mu.Lock()
	// Route async session-memory extraction calls to a trivial reply.
	if req.MaxTokens == sessionMemoryMaxTokens {
		p.mu.Unlock()
		return streamOf(
			agentsdk.StreamEvent{Type: "text_delta", Text: "- noted"},
			agentsdk.StreamEvent{Type: "stop"},
		), nil
	}
	p.systems = append(p.systems, req.System)
	p.foreground++
	first := p.foreground == 1
	p.mu.Unlock()

	if first {
		return streamOf(
			agentsdk.StreamEvent{Type: "tool_use", ToolUse: &agentsdk.ToolUseBlock{ID: "call-1", Name: "greet"}},
			agentsdk.StreamEvent{Type: "text_delta", Text: "{}"},
			agentsdk.StreamEvent{Type: "stop"},
		), nil
	}
	return streamOf(
		agentsdk.StreamEvent{Type: "text_delta", Text: "Release team greeted."},
		agentsdk.StreamEvent{Type: "stop"},
	), nil
}

// capturedSystems returns a copy of the system prompts the provider saw.
func (p *scriptedProvider) capturedSystems() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.systems...)
}

func streamOf(events ...agentsdk.StreamEvent) <-chan agentsdk.StreamEvent {
	ch := make(chan agentsdk.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

// greetTool is a trivial tool so the demo turn has something to execute.
type greetTool struct{}

func (greetTool) Name() string                 { return "greet" }
func (greetTool) Description() string          { return "Greet a named audience." }
func (greetTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (greetTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{Content: "greeted the release team"}, nil
}
