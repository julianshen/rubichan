package shell

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntentClassifierAction(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "action"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	ic := NewIntentClassifier(agentTurn)
	kind, err := ic.Classify(context.Background(), "find all TODO comments")
	assert.NoError(t, err)
	assert.Equal(t, IntentAction, kind)
}

func TestIntentClassifierQuestion(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "question"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	ic := NewIntentClassifier(agentTurn)
	kind, err := ic.Classify(context.Background(), "what is a goroutine")
	assert.NoError(t, err)
	assert.Equal(t, IntentQuestion, kind)
}

func TestIntentClassifierPromptFormat(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedPrompt = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "action"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	ic := NewIntentClassifier(agentTurn)
	_, _ = ic.Classify(context.Background(), "deploy to staging")

	// Prompt should contain the input and classification instructions
	assert.Contains(t, capturedPrompt, "deploy to staging")
	assert.Contains(t, capturedPrompt, "question")
	assert.Contains(t, capturedPrompt, "action")
}

func TestIntentClassifierDefaultsToQuestion(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "I'm not sure, maybe both?"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	ic := NewIntentClassifier(agentTurn)
	kind, err := ic.Classify(context.Background(), "something ambiguous")
	assert.NoError(t, err)
	assert.Equal(t, IntentQuestion, kind, "ambiguous response should default to question")
}

func TestIntentClassifierNilAgent(t *testing.T) {
	t.Parallel()

	ic := NewIntentClassifier(nil)
	kind, err := ic.Classify(context.Background(), "find all files")
	assert.NoError(t, err)
	assert.Equal(t, IntentQuestion, kind, "nil agent should default to question")
}
