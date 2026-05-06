package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

var summaryInterval = 30 * time.Second

func buildSummaryPrompt(previousSummary string) string {
	var prevLine string
	if previousSummary != "" {
		prevLine = fmt.Sprintf("\nPrevious: \"%s\" — say something NEW.\n", previousSummary)
	}

	return fmt.Sprintf(`Describe your most recent action in 3-5 words using present tense (-ing). Name the file or function, not the branch. Do not use tools.
%s
Good: "Reading runAgent.ts"
Good: "Fixing null check in validate.ts"
Good: "Running auth module tests"
Good: "Adding retry logic to fetchUser"

Bad (past tense): "Analyzed the branch diff"
Bad (too vague): "Investigating the issue"
Bad (too long): "Reviewing full branch diff and AgentTool.tsx integration"
Bad (branch name): "Analyzed adam/background-summary branch diff"`, prevLine)
}

// SummaryHandle provides lifecycle control for agent summarization.
type SummaryHandle struct {
	stopFn func()
}

// Stop halts the summarizer.
func (h *SummaryHandle) Stop() {
	if h.stopFn != nil {
		h.stopFn()
	}
}

// ActivitySummarizer generates periodic activity summaries for an agent.
type ActivitySummarizer struct {
	mu              sync.Mutex
	stopped         bool
	previousSummary string
	cancelFn        context.CancelFunc
	timer           *time.Timer
	taskID          string
	onSummary       agentsdk.SummaryCallback
	callModel       func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error)
	systemPrompt    string
	getMessages     func() []provider.Message
}

// StartAgentSummarization begins periodic summarization for an agent.
func StartAgentSummarization(
	taskID string,
	callModel func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error),
	systemPrompt string,
	getMessages func() []provider.Message,
	onSummary agentsdk.SummaryCallback,
) *SummaryHandle {
	s := &ActivitySummarizer{
		taskID:       taskID,
		callModel:    callModel,
		systemPrompt: systemPrompt,
		getMessages:  getMessages,
		onSummary:    onSummary,
	}
	s.scheduleNext()
	return &SummaryHandle{stopFn: s.stop}
}

func (s *ActivitySummarizer) scheduleNext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.timer = time.AfterFunc(summaryInterval, func() {
		go s.runSummary(context.Background())
	})
}

func (s *ActivitySummarizer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
}

func (s *ActivitySummarizer) runSummary(ctx context.Context) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	messages := s.getMessages()
	if len(messages) < 3 {
		s.scheduleNext()
		return
	}

	cleanMessages := filterIncompleteToolCalls(messages)

	innerCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.cancelFn = nil
		s.mu.Unlock()
		s.scheduleNext()
	}()

	prompt := buildSummaryPrompt(s.previousSummary)
	userMsg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}

	agentMessages := make([]provider.Message, len(cleanMessages), len(cleanMessages)+1)
	copy(agentMessages, cleanMessages)
	agentMessages = append(agentMessages, userMsg)

	summaryText, err := s.callModel(innerCtx, agentMessages, s.systemPrompt)
	if err != nil {
		return
	}

	summaryText = strings.TrimSpace(summaryText)
	if summaryText != "" {
		s.mu.Lock()
		s.previousSummary = summaryText
		s.mu.Unlock()
		if s.onSummary != nil {
			s.onSummary(s.taskID, summaryText)
		}
	}
}

// filterIncompleteToolCalls removes partial tool calls before summarization.
// Currently preserves all assistant messages with tool_use blocks; future
// enhancement may filter incomplete tool_use/tool_result pairs.
func filterIncompleteToolCalls(messages []provider.Message) []provider.Message {
	var filtered []provider.Message
	for _, msg := range messages {
		if msg.Role == "assistant" && hasToolUseBlock(msg) {
			filtered = append(filtered, msg)
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func hasToolUseBlock(msg provider.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}
