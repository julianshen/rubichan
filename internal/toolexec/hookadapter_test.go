package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

// --- SkillHookAdapter tests ---

func TestSkillHookAdapterNilRuntimeBeforeToolCall(t *testing.T) {
	adapter := &toolexec.SkillHookAdapter{Runtime: nil}

	cancel, err := adapter.DispatchBeforeToolCall(context.Background(), "shell", json.RawMessage(`{}`))

	assert.NoError(t, err)
	assert.False(t, cancel, "nil runtime should return no cancellation")
}

func TestSkillHookAdapterNilRuntimeAfterToolResult(t *testing.T) {
	adapter := &toolexec.SkillHookAdapter{Runtime: nil}

	modified, err := adapter.DispatchAfterToolResult(context.Background(), "shell", "output", false)

	assert.NoError(t, err)
	assert.Nil(t, modified, "nil runtime should return nil modifications")
}

// --- ResultStoreAdapter tests ---

func TestResultStoreAdapterNilOffloader(t *testing.T) {
	adapter := &toolexec.ResultStoreAdapter{Offloader: nil}

	result, err := adapter.OffloadResult("shell", "id-1", "original content")

	assert.NoError(t, err)
	assert.Equal(t, "original content", result, "nil offloader should return content unchanged")
}

type mockOffloader struct {
	calledToolName  string
	calledToolUseID string
	calledContent   string
	returnValue     string
	returnErr       error
}

func (m *mockOffloader) OffloadResult(toolName, toolUseID, content string) (string, error) {
	m.calledToolName = toolName
	m.calledToolUseID = toolUseID
	m.calledContent = content
	return m.returnValue, m.returnErr
}

func TestResultStoreAdapterDelegatesToOffloader(t *testing.T) {
	offloader := &mockOffloader{
		returnValue: "offloaded-ref",
	}
	adapter := &toolexec.ResultStoreAdapter{Offloader: offloader}

	result, err := adapter.OffloadResult("shell", "id-2", "large output")

	assert.NoError(t, err)
	assert.Equal(t, "offloaded-ref", result)
	assert.Equal(t, "shell", offloader.calledToolName)
	assert.Equal(t, "id-2", offloader.calledToolUseID)
	assert.Equal(t, "large output", offloader.calledContent)
}
