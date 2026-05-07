package tools

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestToolSchemaCacheHit(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}

	c.Set(tool, []byte(`cached`))
	got := c.Get(tool)
	require.Equal(t, []byte(`cached`), got)
}

func TestToolSchemaCacheMiss(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}

	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheStale(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}
	c.Set(tool, []byte(`cached`))

	tool.Description = "Read files and images"
	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheInvalidate(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}
	c.Set(tool, []byte(`cached`))

	c.Invalidate("read")
	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheReset(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}
	c.Set(tool, []byte(`cached`))

	c.Reset()
	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheNilSafe(t *testing.T) {
	var c *ToolSchemaCache
	_ = c.Get(agentsdk.ToolDef{})
	c.Set(agentsdk.ToolDef{}, nil)
	c.Invalidate("")
	c.Reset()
}
