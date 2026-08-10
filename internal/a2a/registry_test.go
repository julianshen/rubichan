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
