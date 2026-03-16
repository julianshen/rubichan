package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// --- context ---

type contextCommand struct {
	getBudget func() agentsdk.ContextBudget
}

// NewContextCommand creates a command that displays context window token usage.
func NewContextCommand(getBudget func() agentsdk.ContextBudget) SlashCommand {
	return &contextCommand{getBudget: getBudget}
}

func (c *contextCommand) Name() string                                       { return "context" }
func (c *contextCommand) Description() string                                { return "Show context window usage" }
func (c *contextCommand) Arguments() []ArgumentDef                           { return nil }
func (c *contextCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *contextCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.getBudget == nil {
		return Result{Output: "Context inspection not available."}, nil
	}

	b := c.getBudget()
	ew := b.EffectiveWindow()
	used := b.UsedTokens()
	remaining := b.RemainingTokens()
	if remaining < 0 {
		remaining = 0
	}

	pct := 0
	if ew > 0 {
		pct = int(b.UsedPercentage() * 100)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Context Usage: %s / %s tokens (%d%%)\n", formatNum(used), formatNum(ew), pct)
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	maxBar := 40
	writeRow := func(label string, tokens int) {
		p := 0
		if ew > 0 {
			p = tokens * 100 / ew
		}
		barLen := tokens * maxBar / max(ew, 1)
		if barLen < 0 {
			barLen = 0
		}
		bar := strings.Repeat("█", barLen)
		fmt.Fprintf(&sb, "  %-18s %6s (%3d%%)  %s\n", label, formatNum(tokens), p, bar)
	}

	writeRow("System prompt", b.SystemPrompt)
	writeRow("Skill prompts", b.SkillPrompts)
	writeRow("Tool definitions", b.ToolDescriptions)
	writeRow("Conversation", b.Conversation)

	remPct := 0
	if ew > 0 {
		remPct = remaining * 100 / ew
	}
	remBar := strings.Repeat("░", remaining*maxBar/max(ew, 1))
	fmt.Fprintf(&sb, "  %-18s %6s (%3d%%)  %s\n", "Remaining", formatNum(remaining), remPct, remBar)

	return Result{Output: sb.String()}, nil
}

func formatNum(n int) string {
	if n < 0 {
		return fmt.Sprintf("-%s", formatNum(-n))
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%s,%03d", formatNum(n/1000), n%1000)
}

// --- compact ---

type compactCommand struct {
	forceCompact func(ctx context.Context) (agentsdk.CompactResult, error)
}

// NewCompactCommand creates a command that forces context compaction.
func NewCompactCommand(forceCompact func(ctx context.Context) (agentsdk.CompactResult, error)) SlashCommand {
	return &compactCommand{forceCompact: forceCompact}
}

func (c *compactCommand) Name() string                                       { return "compact" }
func (c *compactCommand) Description() string                                { return "Compact the context window" }
func (c *compactCommand) Arguments() []ArgumentDef                           { return nil }
func (c *compactCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *compactCommand) Execute(ctx context.Context, _ []string) (Result, error) {
	if c.forceCompact == nil {
		return Result{Output: "Compaction not available."}, nil
	}

	result, err := c.forceCompact(ctx)
	if err != nil {
		return Result{}, err
	}

	if result.BeforeTokens == result.AfterTokens {
		return Result{Output: "No compaction needed — context is within budget."}, nil
	}

	reduction := 0
	if result.BeforeTokens > 0 {
		reduction = (result.BeforeTokens - result.AfterTokens) * 100 / result.BeforeTokens
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Compacted: %s → %s tokens (%d%% reduction)\n",
		formatNum(result.BeforeTokens), formatNum(result.AfterTokens), reduction)
	fmt.Fprintf(&sb, "Messages: %d → %d\n", result.BeforeMsgCount, result.AfterMsgCount)
	if len(result.StrategiesRun) > 0 {
		fmt.Fprintf(&sb, "Strategies: %s\n", strings.Join(result.StrategiesRun, ", "))
	}

	return Result{Output: sb.String()}, nil
}
