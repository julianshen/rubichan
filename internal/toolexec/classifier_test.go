package toolexec_test

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

func TestClassifyBuiltinTools(t *testing.T) {
	c := toolexec.NewClassifier(nil)

	tests := []struct {
		toolName string
		want     toolexec.Category
	}{
		// Exact matches
		{"shell", toolexec.CategoryBash},
		{"file", toolexec.CategoryFileRead},
		{"search", toolexec.CategorySearch},
		{"process", toolexec.CategoryAgent},
		{"compact_context", toolexec.CategoryAgent},
		{"read_result", toolexec.CategoryAgent},
		{"notes", toolexec.CategoryAgent},
		{"tool_search", toolexec.CategoryAgent},
		{"task", toolexec.CategoryAgent},
		{"list_tasks", toolexec.CategoryAgent},
		{"db_query", toolexec.CategoryNet},

		// Prefix matches
		{"git-status", toolexec.CategoryGit},
		{"git_status", toolexec.CategoryGit},
		{"git-diff", toolexec.CategoryGit},
		{"git-commit", toolexec.CategoryGit},
		{"http_get", toolexec.CategoryNet},
		{"browser_open", toolexec.CategoryNet},
		{"xcode_build", toolexec.CategoryPlatform},
		{"xcode_test", toolexec.CategoryPlatform},
		{"mcp-server-tool", toolexec.CategoryMCP},
		{"mcp-fetch", toolexec.CategoryMCP},
		{"mcp_browser_tool", toolexec.CategoryMCP},

		// Default fallback
		{"unknown_tool", toolexec.CategorySkill},
		{"my-custom-tool", toolexec.CategorySkill},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := c.Classify(tt.toolName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassifyWithOverrides(t *testing.T) {
	overrides := map[string]toolexec.Category{
		"shell":       toolexec.CategoryNet, // override a built-in
		"custom_tool": toolexec.CategoryGit, // override a default-to-skill
	}
	c := toolexec.NewClassifier(overrides)

	// Overrides take precedence over built-in mappings.
	assert.Equal(t, toolexec.CategoryNet, c.Classify("shell"))
	assert.Equal(t, toolexec.CategoryGit, c.Classify("custom_tool"))

	// Non-overridden built-ins still work.
	assert.Equal(t, toolexec.CategoryFileRead, c.Classify("file"))
	assert.Equal(t, toolexec.CategorySearch, c.Classify("search"))
}

func TestClassifierMiddleware(t *testing.T) {
	c := toolexec.NewClassifier(nil)
	mw := toolexec.ClassifierMiddleware(c)

	var captured toolexec.Category

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		captured = toolexec.CategoryFromContext(ctx)
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-1",
		Name: "shell",
	})

	assert.Equal(t, toolexec.CategoryBash, captured)
	assert.Equal(t, "ok", result.Content)
}

func TestCategoryFromContextDefault(t *testing.T) {
	// When no category is set in context, CategoryFromContext returns the zero value.
	cat := toolexec.CategoryFromContext(context.Background())
	assert.Equal(t, toolexec.Category(""), cat)
}
