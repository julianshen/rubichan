package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     ToolType
	}{
		{"shell", "shell", ToolTypeShell},
		{"file", "file", ToolTypeFile},
		{"search", "search", ToolTypeSearch},
		{"process", "process", ToolTypeProcess},
		{"task", "task", ToolTypeSubagent},
		{"unknown tool", "custom_tool", ToolTypeDefault},
		{"git tool", "git_status", ToolTypeDefault},
		{"notes tool", "notes", ToolTypeDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyTool(tt.toolName))
		})
	}
}

func TestToolTypeIcon(t *testing.T) {
	tests := []struct {
		name string
		tt   ToolType
		want string
	}{
		{"shell", ToolTypeShell, "$ "},
		{"file", ToolTypeFile, "~ "},
		{"search", ToolTypeSearch, "? "},
		{"process", ToolTypeProcess, "* "},
		{"subagent", ToolTypeSubagent, "> "},
		{"default", ToolTypeDefault, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.tt.Icon())
		})
	}
}
