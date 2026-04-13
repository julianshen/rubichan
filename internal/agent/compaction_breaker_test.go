package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// alwaysFailingStrategy returns a non-nil error from every Compact call.
type alwaysFailingStrategy struct{ name string }

func (s *alwaysFailingStrategy) Name() string { return s.name }
func (s *alwaysFailingStrategy) Compact(_ context.Context, msgs []provider.Message, _ int) ([]provider.Message, error) {
	return msgs, errors.New("boom")
}

func TestCompactionCircuitBreakerTripsAfterThreeFailures(t *testing.T) {
	t.Parallel()
	cm := NewContextManager(100, 10) // tiny budget to force compaction
	cm.SetStrategies([]agentsdk.CompactionStrategy{&alwaysFailingStrategy{name: "fail"}})

	conv := NewConversation("system")
	for i := 0; i < 50; i++ {
		conv.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}

	for i := 0; i < 2; i++ {
		err := cm.Compact(context.Background(), conv)
		if err != nil {
			t.Fatalf("attempt %d: want nil, got %v", i+1, err)
		}
	}
	if err := cm.Compact(context.Background(), conv); !errors.Is(err, ErrCompactionExhausted) {
		t.Fatalf("want ErrCompactionExhausted on third failure, got %v", err)
	}
}

func TestCompactionCircuitBreakerResetsOnSuccess(t *testing.T) {
	t.Parallel()
	cm := NewContextManager(100, 10)
	succeedNext := false
	strat := &fakeStrategyWithToggle{succeed: &succeedNext}
	cm.SetStrategies([]agentsdk.CompactionStrategy{strat})

	conv := NewConversation("system")
	for i := 0; i < 50; i++ {
		conv.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}

	_ = cm.Compact(context.Background(), conv)
	_ = cm.Compact(context.Background(), conv)
	succeedNext = true
	if err := cm.Compact(context.Background(), conv); err != nil {
		t.Fatalf("success should reset counter, got %v", err)
	}
	succeedNext = false
	for i := 0; i < 40; i++ {
		conv.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}
	// After reset, three new failures needed to trip.
	_ = cm.Compact(context.Background(), conv)
	_ = cm.Compact(context.Background(), conv)
	if err := cm.Compact(context.Background(), conv); !errors.Is(err, ErrCompactionExhausted) {
		t.Fatalf("want trip after 3 new failures, got %v", err)
	}
}

// fakeStrategyWithToggle succeeds (by halving the message slice) iff *succeed is true.
type fakeStrategyWithToggle struct{ succeed *bool }

func (s *fakeStrategyWithToggle) Name() string { return "toggle" }
func (s *fakeStrategyWithToggle) Compact(_ context.Context, msgs []provider.Message, _ int) ([]provider.Message, error) {
	if *s.succeed {
		if len(msgs) > 2 {
			return msgs[len(msgs)/2:], nil
		}
		return msgs, nil
	}
	return msgs, errors.New("boom")
}

// TestRunLoopExitsWithCompactionFailed verifies the agent loop surfaces
// ErrCompactionExhausted via Turn() (either as a sync error from the
// pre-turn Compact call, or as ExitCompactionFailed on the done event).
func TestRunLoopExitsWithCompactionFailed(t *testing.T) {
	t.Parallel()

	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "ok"},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	ag := New(mp, reg, autoApprove, cfg)

	// Replace the ContextManager with one that uses an always-failing
	// strategy and a tiny budget so ShouldCompact fires.
	cm := NewContextManager(100, 10)
	cm.SetStrategies([]agentsdk.CompactionStrategy{&alwaysFailingStrategy{name: "fail"}})
	ag.context = cm
	ag.customStrategies = true

	// Bloat the conversation so compaction actually runs.
	for i := 0; i < 50; i++ {
		ag.conversation.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}

	// Pre-trip the breaker to failure count = 2.
	_ = cm.Compact(context.Background(), ag.conversation)
	_ = cm.Compact(context.Background(), ag.conversation)

	ch, err := ag.Turn(context.Background(), "trigger")
	if err != nil {
		if !errors.Is(err, ErrCompactionExhausted) {
			t.Fatalf("sync Turn error: want ErrCompactionExhausted, got %v", err)
		}
		return // synchronous path taken — breaker wired correctly
	}
	var last TurnEvent
	for ev := range ch {
		last = ev
	}
	if last.Type != "done" {
		t.Fatalf("want last event type=done, got %q", last.Type)
	}
	if last.ExitReason != agentsdk.ExitCompactionFailed {
		t.Fatalf("want ExitCompactionFailed, got %v", last.ExitReason)
	}
}
