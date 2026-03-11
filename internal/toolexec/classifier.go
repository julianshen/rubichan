package toolexec

import (
	"context"
	"strings"
)

// Category identifies the kind of operation a tool performs.
type Category string

const (
	CategoryBash     Category = "bash"
	CategoryFileRead Category = "file_read"
	CategorySearch   Category = "search"
	CategoryGit      Category = "git"
	CategoryNet      Category = "net"
	CategoryMCP      Category = "mcp"
	CategoryPlatform Category = "platform"
	CategorySkill    Category = "skill"
	CategoryAgent    Category = "agent"
)

// contextKey is an unexported type to prevent collisions in context values.
type contextKey struct{}

// categoryKey is the context key for Category values.
var categoryKey = contextKey{}

// WithCategory returns a new context carrying the given Category.
func WithCategory(ctx context.Context, cat Category) context.Context {
	return context.WithValue(ctx, categoryKey, cat)
}

// CategoryFromContext extracts the Category from the context.
// Returns the zero value ("") when no category has been set.
func CategoryFromContext(ctx context.Context) Category {
	cat, _ := ctx.Value(categoryKey).(Category)
	return cat
}

// builtinExact maps exact tool names to categories.
var builtinExact = map[string]Category{
	"shell":           CategoryBash,
	"file":            CategoryFileRead,
	"search":          CategorySearch,
	"db_query":        CategoryNet,
	"process":         CategoryAgent,
	"compact_context": CategoryAgent,
	"read_result":     CategoryAgent,
	"notes":           CategoryAgent,
	"tool_search":     CategoryAgent,
	"task":            CategoryAgent,
	"list_tasks":      CategoryAgent,
}

// builtinPrefixes maps tool name prefixes to categories.
// Order matters: first match wins.
var builtinPrefixes = []struct {
	prefix   string
	category Category
}{
	{"git-", CategoryGit},
	{"git_", CategoryGit},
	{"http_", CategoryNet},
	{"browser_", CategoryNet},
	{"xcode_", CategoryPlatform},
	{"mcp-", CategoryMCP},
	{"mcp_", CategoryMCP},
}

// Classifier maps tool names to categories using built-in rules and
// optional overrides.
type Classifier struct {
	overrides map[string]Category
}

// NewClassifier creates a Classifier. If overrides is non-nil, those
// mappings take precedence over built-in rules.
func NewClassifier(overrides map[string]Category) *Classifier {
	return &Classifier{overrides: overrides}
}

// Classify returns the Category for the given tool name. It checks
// overrides first, then exact built-in mappings, then prefix-based
// mappings, and falls back to CategorySkill.
func (c *Classifier) Classify(toolName string) Category {
	// Overrides take precedence.
	if c.overrides != nil {
		if cat, ok := c.overrides[toolName]; ok {
			return cat
		}
	}

	// Exact built-in match.
	if cat, ok := builtinExact[toolName]; ok {
		return cat
	}

	// Prefix-based match.
	for _, bp := range builtinPrefixes {
		if strings.HasPrefix(toolName, bp.prefix) {
			return bp.category
		}
	}

	return CategorySkill
}

// ClassifierMiddleware returns a Middleware that attaches the tool's
// category to the context before calling the next handler.
func ClassifierMiddleware(c *Classifier) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			cat := c.Classify(tc.Name)
			ctx = WithCategory(ctx, cat)
			return next(ctx, tc)
		}
	}
}
