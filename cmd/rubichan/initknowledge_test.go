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
		projectName: "TestProject",
		backends:    []string{"Go"},
		frontends:   []string{"React"},
		databases:   []string{"PostgreSQL"},
		infras:      []string{"Docker"},
		archStyle:   "Microservices",
		painPoints:  []string{"scaling", "testing"},
		teamSize:    "small",
		teamComp:    "fullstack",
		isExisting:  true,
	}

	// Verify mock questioner implements interface
	require.NoError(t, runInitKnowledgeGraphWithQuestioner(context.Background(), tmpDir, mockQ))
}

// MockQuestioner is a test double for the bootstrap questioner.
type MockQuestioner struct {
	projectName string
	backends    []string
	frontends   []string
	databases   []string
	infras      []string
	archStyle   string
	painPoints  []string
	teamSize    string
	teamComp    string
	isExisting  bool
}

// AskString implements the Questioner interface.
func (m *MockQuestioner) AskString(prompt string) (string, error) {
	switch prompt {
	case "What is your project name?":
		return m.projectName, nil
	case "Describe your pain points (comma-separated):":
		return "scaling, testing", nil
	case "Is this an existing project?":
		if m.isExisting {
			return "yes", nil
		}
		return "no", nil
	default:
		return "", nil
	}
}

// AskChoice implements the Questioner interface.
func (m *MockQuestioner) AskChoice(prompt string, options []string) (string, error) {
	switch prompt {
	case "What is your architecture style?":
		return m.archStyle, nil
	case "What is your team size?":
		return m.teamSize, nil
	case "What is your team composition?":
		return m.teamComp, nil
	default:
		if len(options) > 0 {
			return options[0], nil
		}
		return "", nil
	}
}

// AskMultiSelect implements the Questioner interface.
func (m *MockQuestioner) AskMultiSelect(prompt string, options []string) ([]string, error) {
	switch prompt {
	case "Select backend technologies:":
		return m.backends, nil
	case "Select frontend technologies:":
		return m.frontends, nil
	case "Select database technologies:":
		return m.databases, nil
	case "Select infrastructure technologies:":
		return m.infras, nil
	default:
		return []string{}, nil
	}
}
