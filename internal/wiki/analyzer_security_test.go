package wiki

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityAnalyzer_Name(t *testing.T) {
	a := NewSecurityAnalyzer(&mockLLMCompleter{})
	assert.Equal(t, "security", a.Name())
}

func TestSecurityAnalyzer_ProducesThreeDocs(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"authentication": "Auth mechanisms: JWT tokens for API authentication.\n\n```mermaid\nsequenceDiagram\n    Client->>Server: POST /login\n    Server-->>Client: JWT token\n```\n",
			"STRIDE":         "Threat analysis using STRIDE model.\n\n```mermaid\ngraph TD\n    A[Attacker] --> B[Spoof Identity]\n```\n",
			"sensitive data": "Sensitive data is encrypted at rest using AES-256.\n\n```mermaid\ngraph LR\n    User --> App --> DB\n```\n",
		},
	}
	a := NewSecurityAnalyzer(llm)

	input := AnalyzerInput{
		Architecture:   "Layered architecture with provider, tool, and agent packages.",
		ModuleAnalyses: []ModuleAnalysis{{Module: "internal/auth", Summary: "Auth module"}},
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, out.Documents, 3)

	paths := map[string]bool{}
	for _, d := range out.Documents {
		paths[d.Path] = true
	}
	assert.True(t, paths["security/auth-and-access.md"])
	assert.True(t, paths["security/threat-model.md"])
	assert.True(t, paths["security/data-flow.md"])
}

func TestSecurityAnalyzer_EmptyArchitecture(t *testing.T) {
	a := NewSecurityAnalyzer(&mockLLMCompleter{})

	out, err := a.Analyze(context.Background(), AnalyzerInput{Architecture: ""})
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
	assert.Empty(t, out.Diagrams)
}

func TestSecurityAnalyzer_PartialLLMFailure(t *testing.T) {
	// STRIDE sub-prompt fails; auth and data-flow succeed.
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"authentication": "Auth mechanisms: API keys and OAuth2.",
			"sensitive data": "Data is encrypted in transit via TLS.",
		},
	}

	// Override the STRIDE call to fail by using a selective failing completer.
	failingLLM := &selectiveFailLLMCompleter{
		inner:  llm,
		failOn: "STRIDE",
	}

	a := NewSecurityAnalyzer(failingLLM)
	input := AnalyzerInput{Architecture: "Some architecture."}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)

	// Two docs should be produced (auth and data-flow); threat-model fails silently.
	require.Len(t, out.Documents, 2)

	paths := map[string]bool{}
	for _, d := range out.Documents {
		paths[d.Path] = true
	}
	assert.True(t, paths["security/auth-and-access.md"])
	assert.True(t, paths["security/data-flow.md"])
	assert.False(t, paths["security/threat-model.md"])
}

func TestSecurityAnalyzer_ExtractsMermaidDiagram(t *testing.T) {
	mermaidContent := "sequenceDiagram\n    Client->>Server: authenticate\n    Server-->>Client: token"
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"authentication": "Auth uses JWT.\n\n```mermaid\n" + mermaidContent + "\n```\n",
			"STRIDE":         "STRIDE analysis done.",
			"sensitive data": "Data encrypted.",
		},
	}
	a := NewSecurityAnalyzer(llm)
	input := AnalyzerInput{Architecture: "Microservices with JWT auth."}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)

	// At least one diagram should be extracted (from auth sub-prompt).
	require.NotEmpty(t, out.Diagrams)

	found := false
	for _, d := range out.Diagrams {
		if d.Title == "Auth Flow" {
			found = true
			assert.Equal(t, "sequence", d.Type)
			assert.Contains(t, d.Content, "Client->>Server")
		}
	}
	assert.True(t, found, "expected diagram with title 'Auth Flow'")
}

// selectiveFailLLMCompleter delegates to inner but returns an error when the
// prompt contains the failOn substring.
type selectiveFailLLMCompleter struct {
	inner  LLMCompleter
	failOn string
}

func (s *selectiveFailLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	for _, kw := range []string{s.failOn} {
		for i := 0; i < len(prompt)-len(kw)+1; i++ {
			if prompt[i:i+len(kw)] == kw {
				return "", errors.New("forced LLM failure")
			}
		}
	}
	return s.inner.Complete(ctx, prompt)
}
