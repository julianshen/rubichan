package shell

import (
	"context"
	"strings"
)

// IntentKind represents whether a ? query is a question or an action.
type IntentKind int

const (
	IntentQuestion IntentKind = iota // Conversational response
	IntentAction                     // Generate and run a script
)

// IntentClassifier uses the LLM to classify ? queries as questions or actions.
type IntentClassifier struct {
	agentTurn AgentTurnFunc
}

// NewIntentClassifier creates an intent classifier.
func NewIntentClassifier(agentTurn AgentTurnFunc) *IntentClassifier {
	return &IntentClassifier{agentTurn: agentTurn}
}

const intentClassifyPrompt = `Classify the following user input as either "question" or "action".

- "question": The user is asking for information, explanation, or understanding.
- "action": The user wants to perform a task that can be accomplished by running a shell script.

Examples:
- "what is a goroutine" → question
- "explain the auth flow" → question
- "how does Docker networking work" → question
- "find all TODO comments" → action
- "count lines of code by language" → action
- "delete all .tmp files" → action
- "deploy to staging" → action
- "list all open ports" → action

Reply with ONLY the single word "question" or "action".

User input: `

// Classify determines if the input is a question or an action request.
// Returns IntentQuestion on error or ambiguity (safe default).
func (ic *IntentClassifier) Classify(ctx context.Context, input string) (IntentKind, error) {
	if ic.agentTurn == nil {
		return IntentQuestion, nil
	}

	prompt := intentClassifyPrompt + input

	events, err := ic.agentTurn(ctx, prompt)
	if err != nil {
		return IntentQuestion, nil
	}

	text := strings.TrimSpace(strings.ToLower(collectTurnText(events)))
	if text == "action" {
		return IntentAction, nil
	}
	// Default to question (safe fallback)
	return IntentQuestion, nil
}
