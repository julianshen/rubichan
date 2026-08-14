package a2a

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistryMatchByTagOverlap(t *testing.T) {
	reg := NewRegistry()
	reg.Add("weather-bot", AgentCard{
		Name:   "weather-bot",
		Skills: []Skill{{ID: "forecast", Name: "Forecast", Description: "weather forecasts", Tags: []string{"weather", "forecast"}}},
	})
	reg.Add("code-bot", AgentCard{
		Name:   "code-bot",
		Skills: []Skill{{ID: "review", Name: "Review", Description: "reviews code", Tags: []string{"code", "review"}}},
	})

	matches := reg.Match("what's the weather forecast for tomorrow")
	assert.Equal(t, []string{"weather-bot"}, matches)
}

func TestRegistryMatchNoneFound(t *testing.T) {
	reg := NewRegistry()
	reg.Add("code-bot", AgentCard{Name: "code-bot", Skills: []Skill{{ID: "review", Tags: []string{"code"}}}})

	assert.Empty(t, reg.Match("book me a flight to Tokyo"))
}

func TestRegistryMatchSortsByScoreDescendingStableOnTies(t *testing.T) {
	reg := NewRegistry()

	// Register 4 agents in insertion order such that their scores are NOT
	// in descending order, forcing the insertion-sort loop to do real work.
	// Task "code review" will produce scores: tie1=2, tie2=2, low=1, high=4
	reg.Add("agent-tie1", AgentCard{
		Name:   "agent-tie1",
		Skills: []Skill{{ID: "s1", Name: "S1", Description: "code review", Tags: []string{}}}, // 2 words
	})
	reg.Add("agent-tie2", AgentCard{
		Name:   "agent-tie2",
		Skills: []Skill{{ID: "s2", Name: "S2", Description: "code review", Tags: []string{}}}, // 2 words (tied with tie1)
	})
	reg.Add("agent-low", AgentCard{
		Name:   "agent-low",
		Skills: []Skill{{ID: "s3", Name: "S3", Description: "testing", Tags: []string{}}}, // 0 words
	})
	reg.Add("agent-high", AgentCard{
		Name:   "agent-high",
		Skills: []Skill{{ID: "s4", Name: "S4", Description: "code", Tags: []string{"review"}}, {ID: "s5", Name: "S5", Description: "more code", Tags: []string{}}}, // 4 words total
	})

	matches := reg.Match("code review")

	// Expected order: agent-high (4), then agent-tie1 (2), agent-tie2 (2),
	// then agent-low would not be included as it has no matches.
	// Tied agents tie1 and tie2 should preserve their insertion order.
	assert.Equal(t, []string{"agent-high", "agent-tie1", "agent-tie2"}, matches)
}

func TestRegistryMatchExcludesPunctuationOnlyOverlap(t *testing.T) {
	reg := NewRegistry()

	// Register an agent whose skill description is only punctuation.
	reg.Add("punct-agent", AgentCard{
		Name:   "punct-agent",
		Skills: []Skill{{ID: "s1", Name: "S1", Description: "...", Tags: []string{}}},
	})

	// Match against a task that contains that same punctuation token.
	// The agent should NOT match because "..." trims to empty string,
	// and empty-string matches should not count.
	matches := reg.Match("hello world ...")

	assert.Empty(t, matches, "agent with only punctuation in description should not match")
}
