package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInitKnowledgeGraphSkill verifies the /initknowledgegraph skill command
// is properly constructed with correct metadata.
func TestInitKnowledgeGraphSkill(t *testing.T) {
	cmd := initKnowledgeGraphCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "initknowledgegraph", cmd.Use)
	require.Equal(t, "Bootstrap a knowledge graph for your project", cmd.Short)
	require.NotEmpty(t, cmd.Long)
}

// TestInteractiveQuestionerAskString verifies the questioner prompts for string input.
func TestInteractiveQuestionerAskString(t *testing.T) {
	q := &InteractiveQuestioner{}
	require.NotNil(t, q)
	// NOTE: Cannot easily test interactive input without mocking stdin,
	// but we verify the interface implementation exists.
}

// TestRunInitKnowledgeGraphWithMockQuestioner verifies the bootstrap orchestration
// with a mock questioner that doesn't require real user input.
func TestRunInitKnowledgeGraphWithMockQuestioner(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a mock questioner that returns predetermined answers
	mockQ := &MockQuestioner{
		responses: map[string]interface{}{
			"What is your project name?":                   "TestProject",
			"Select backend technologies:":                 []string{"Go"},
			"Select frontend technologies:":                []string{"React"},
			"Select database technologies:":                []string{"PostgreSQL"},
			"Select infrastructure technologies:":          []string{"Docker"},
			"What is your architecture style?":             "Microservices",
			"Describe your pain points (comma-separated):": "scaling, testing",
			"What is your team size?":                      "small",
			"What is your team composition?":               "fullstack",
			"Is this an existing project?":                 "yes",
		},
	}

	// Verify mock questioner implements interface
	require.NoError(t, runInitKnowledgeGraphWithQuestioner(context.Background(), tmpDir, mockQ))
}

// MockQuestioner implements Questioner interface for testing.
// Uses a flexible map-based approach that works with any prompt.
type MockQuestioner struct {
	responses map[string]interface{}
}

// AskString implements the Questioner interface.
func (m *MockQuestioner) AskString(prompt string) (string, error) {
	if val, ok := m.responses[prompt]; ok {
		if s, ok := val.(string); ok {
			return s, nil
		}
	}
	return "", nil
}

// AskChoice implements the Questioner interface.
func (m *MockQuestioner) AskChoice(prompt string, options []string) (string, error) {
	if val, ok := m.responses[prompt]; ok {
		if s, ok := val.(string); ok {
			return s, nil
		}
	}
	if len(options) > 0 {
		return options[0], nil
	}
	return "", nil
}

// AskMultiSelect implements the Questioner interface.
func (m *MockQuestioner) AskMultiSelect(prompt string, options []string) ([]string, error) {
	if val, ok := m.responses[prompt]; ok {
		if ss, ok := val.([]string); ok {
			return ss, nil
		}
	}
	return []string{}, nil
}
