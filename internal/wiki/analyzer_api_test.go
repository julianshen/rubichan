package wiki

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIAnalyzer_Name(t *testing.T) {
	a := NewAPIAnalyzer(&mockLLMCompleter{})
	assert.Equal(t, "api", a.Name())
}

func TestAPIAnalyzer_NoPatterns(t *testing.T) {
	a := NewAPIAnalyzer(&mockLLMCompleter{})

	out, err := a.Analyze(context.Background(), AnalyzerInput{})
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
	assert.Empty(t, out.Diagrams)
}

func TestAPIAnalyzer_HTTPPatterns(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"http": "## HTTP Endpoints\n\n- GET /users — ListUsers\n- POST /users — CreateUser",
		},
	}
	a := NewAPIAnalyzer(llm)

	input := AnalyzerInput{
		APIPatterns: []APIPattern{
			{Kind: "http", Method: "GET", Path: "/users", Handler: "ListUsers", File: "handler.go", Line: 10, Language: "go"},
			{Kind: "http", Method: "POST", Path: "/users", Handler: "CreateUser", File: "handler.go", Line: 20, Language: "go"},
		},
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)

	// Should produce _index.md and http-endpoints.md
	require.Len(t, out.Documents, 2)

	paths := docPaths(out.Documents)
	assert.Contains(t, paths, "api/_index.md")
	assert.Contains(t, paths, "api/http-endpoints.md")

	httpDoc := findDoc(out.Documents, "api/http-endpoints.md")
	require.NotNil(t, httpDoc)
	assert.Contains(t, httpDoc.Content, "ListUsers")
}

func TestAPIAnalyzer_MultipleKinds(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"http":   "## HTTP Endpoints\n\nGET /health",
			"grpc":   "## gRPC Services\n\nUserService",
			"export": "## Public Interfaces\n\nClient interface",
		},
	}
	a := NewAPIAnalyzer(llm)

	input := AnalyzerInput{
		APIPatterns: []APIPattern{
			{Kind: "http", Method: "GET", Path: "/health", Handler: "HealthCheck", File: "http.go", Line: 5, Language: "go"},
			{Kind: "grpc", Path: "UserService", Handler: "GetUser", File: "grpc.go", Line: 15, Language: "go"},
			{Kind: "export", Path: "Client", Handler: "NewClient", File: "client.go", Line: 1, Language: "go"},
		},
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)

	// _index.md + 3 kind docs
	require.Len(t, out.Documents, 4)

	paths := docPaths(out.Documents)
	assert.Contains(t, paths, "api/_index.md")
	assert.Contains(t, paths, "api/http-endpoints.md")
	assert.Contains(t, paths, "api/grpc-services.md")
	assert.Contains(t, paths, "api/public-interfaces.md")
}

func TestAPIAnalyzer_LLMError(t *testing.T) {
	a := NewAPIAnalyzer(&errorLLMCompleter{err: errors.New("LLM unavailable")})

	input := AnalyzerInput{
		APIPatterns: []APIPattern{
			{Kind: "http", Method: "GET", Path: "/ping", Handler: "Ping", File: "handler.go", Line: 1, Language: "go"},
		},
	}

	// LLM errors are non-fatal: should return empty output (no docs beyond _index), no error.
	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	// _index.md is always produced when there are patterns; kind docs are skipped on LLM error.
	for _, doc := range out.Documents {
		assert.Equal(t, "api/_index.md", doc.Path, "only _index.md should be present on LLM error")
	}
}

// ---------- helpers ----------

func docPaths(docs []Document) []string {
	paths := make([]string, len(docs))
	for i, d := range docs {
		paths[i] = d.Path
	}
	return paths
}

func findDoc(docs []Document, path string) *Document {
	for i := range docs {
		if docs[i].Path == path {
			return &docs[i]
		}
	}
	return nil
}
