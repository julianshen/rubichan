package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSkillManager implements SkillManagerAccess for testing.
type mockSkillManager struct {
	searchFn  func(ctx context.Context, query string) ([]SkillSearchResult, error)
	installFn func(ctx context.Context, source string) (SkillInstallResult, error)
	listFn    func() ([]SkillListEntry, error)
	removeFn  func(name string) error
}

func (m *mockSkillManager) Search(ctx context.Context, query string) ([]SkillSearchResult, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, query)
	}
	return nil, nil
}

func (m *mockSkillManager) Install(ctx context.Context, source string) (SkillInstallResult, error) {
	if m.installFn != nil {
		return m.installFn(ctx, source)
	}
	return SkillInstallResult{}, nil
}

func (m *mockSkillManager) List() ([]SkillListEntry, error) {
	if m.listFn != nil {
		return m.listFn()
	}
	return nil, nil
}

func (m *mockSkillManager) Remove(name string) error {
	if m.removeFn != nil {
		return m.removeFn(name)
	}
	return nil
}

func TestSkillManagerToolName(t *testing.T) {
	tool := NewSkillManagerTool(nil)
	assert.Equal(t, "skill_manager", tool.Name())
}

func TestSkillManagerToolDescription(t *testing.T) {
	tool := NewSkillManagerTool(nil)
	assert.NotEmpty(t, tool.Description())
}

func TestSkillManagerToolInputSchema(t *testing.T) {
	tool := NewSkillManagerTool(nil)
	schema := tool.InputSchema()
	require.NotNil(t, schema)

	var parsed map[string]interface{}
	err := json.Unmarshal(schema, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "object", parsed["type"])

	props, ok := parsed["properties"].(map[string]interface{})
	require.True(t, ok)

	action, ok := props["action"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", action["type"])

	enumVals, ok := action["enum"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, enumVals, "search")
	assert.Contains(t, enumVals, "install")
	assert.Contains(t, enumVals, "list")
	assert.Contains(t, enumVals, "remove")
}

func TestSkillManagerToolNilManager(t *testing.T) {
	tool := NewSkillManagerTool(nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not initialized")
}

func TestSkillManagerToolInvalidJSON(t *testing.T) {
	tool := NewSkillManagerTool(&mockSkillManager{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestSkillManagerToolUnknownAction(t *testing.T) {
	tool := NewSkillManagerTool(&mockSkillManager{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"bogus"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown action")
}

// Compile-time check that SkillManagerTool satisfies Tool.
var _ Tool = (*SkillManagerTool)(nil)

func TestSkillManagerSearch(t *testing.T) {
	mock := &mockSkillManager{
		searchFn: func(_ context.Context, query string) ([]SkillSearchResult, error) {
			assert.Equal(t, "kubernetes", query)
			return []SkillSearchResult{
				{Name: "kubernetes", Version: "1.2.0", Description: "kubectl wrapper"},
				{Name: "k8s-debug", Version: "0.3.1", Description: "debug pods"},
			}, nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"kubernetes"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "kubernetes")
	assert.Contains(t, result.Content, "1.2.0")
	assert.Contains(t, result.Content, "k8s-debug")
}

func TestSkillManagerSearchEmpty(t *testing.T) {
	mock := &mockSkillManager{
		searchFn: func(_ context.Context, _ string) ([]SkillSearchResult, error) {
			return nil, nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"nonexistent"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "No skills found")
}

func TestSkillManagerSearchMissingQuery(t *testing.T) {
	tool := NewSkillManagerTool(&mockSkillManager{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "query is required")
}

func TestSkillManagerSearchError(t *testing.T) {
	mock := &mockSkillManager{
		searchFn: func(_ context.Context, _ string) ([]SkillSearchResult, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"test"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "connection refused")
}

func TestSkillManagerList(t *testing.T) {
	mock := &mockSkillManager{
		listFn: func() ([]SkillListEntry, error) {
			return []SkillListEntry{
				{Name: "kubernetes", Version: "1.2.0", Source: "registry", InstalledAt: "2026-03-20"},
				{Name: "ddd-expert", Version: "0.1.0", Source: "local", InstalledAt: "2026-03-18"},
			}, nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "kubernetes")
	assert.Contains(t, result.Content, "1.2.0")
	assert.Contains(t, result.Content, "ddd-expert")
}

func TestSkillManagerListEmpty(t *testing.T) {
	mock := &mockSkillManager{
		listFn: func() ([]SkillListEntry, error) {
			return nil, nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "No skills installed")
}

func TestSkillManagerListError(t *testing.T) {
	mock := &mockSkillManager{
		listFn: func() ([]SkillListEntry, error) {
			return nil, fmt.Errorf("database locked")
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "database locked")
}

func TestSkillManagerInstall(t *testing.T) {
	mock := &mockSkillManager{
		installFn: func(_ context.Context, source string) (SkillInstallResult, error) {
			assert.Equal(t, "kubernetes@1.2.0", source)
			return SkillInstallResult{Name: "kubernetes", Version: "1.2.0", Activated: true}, nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"install","source":"kubernetes@1.2.0"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "kubernetes")
	assert.Contains(t, result.Content, "1.2.0")
}

func TestSkillManagerInstallMissingSource(t *testing.T) {
	tool := NewSkillManagerTool(&mockSkillManager{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"install"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "source is required")
}

func TestSkillManagerInstallError(t *testing.T) {
	mock := &mockSkillManager{
		installFn: func(_ context.Context, _ string) (SkillInstallResult, error) {
			return SkillInstallResult{}, fmt.Errorf("skill not found in registry")
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"install","source":"nonexistent"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "skill not found in registry")
}

func TestSkillManagerInstallShowsActivation(t *testing.T) {
	mock := &mockSkillManager{
		installFn: func(_ context.Context, _ string) (SkillInstallResult, error) {
			return SkillInstallResult{Name: "test-skill", Version: "1.0.0", Activated: true}, nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"install","source":"test-skill"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "activated")
}

func TestSkillManagerRemove(t *testing.T) {
	mock := &mockSkillManager{
		removeFn: func(name string) error {
			assert.Equal(t, "kubernetes", name)
			return nil
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"remove","name":"kubernetes"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "kubernetes")
	assert.Contains(t, result.Content, "removed")
}

func TestSkillManagerRemoveMissingName(t *testing.T) {
	tool := NewSkillManagerTool(&mockSkillManager{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"remove"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "name is required")
}

func TestSkillManagerRemoveError(t *testing.T) {
	mock := &mockSkillManager{
		removeFn: func(_ string) error {
			return fmt.Errorf("skill not installed")
		},
	}
	tool := NewSkillManagerTool(mock)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"remove","name":"nonexistent"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "skill not installed")
}
