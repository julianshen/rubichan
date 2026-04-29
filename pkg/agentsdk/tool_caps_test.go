package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockInputSafeTool struct {
	staticSafe bool
	inputSafe  func(json.RawMessage) bool
}

func (m *mockInputSafeTool) IsConcurrencySafe() bool { return m.staticSafe }
func (m *mockInputSafeTool) IsConcurrencySafeForInput(input json.RawMessage) bool {
	if m.inputSafe != nil {
		return m.inputSafe(input)
	}
	return false
}

func TestInputConcurrencySafeTool_Interface(t *testing.T) {
	var _ InputConcurrencySafeTool = &mockInputSafeTool{}
}

func TestInputConcurrencySafeTool_OverridesStatic(t *testing.T) {
	tool := &mockInputSafeTool{
		staticSafe: true,
		inputSafe: func(input json.RawMessage) bool {
			return string(input) == `{"cmd":"cat file"}`
		},
	}
	assert.True(t, tool.IsConcurrencySafeForInput(json.RawMessage(`{"cmd":"cat file"}`)))
	assert.False(t, tool.IsConcurrencySafeForInput(json.RawMessage(`{"cmd":"rm file"}`)))
	assert.True(t, tool.IsConcurrencySafe(), "static safety should still be true")
}

func TestInputConcurrencySafeTool_NilFunc(t *testing.T) {
	tool := &mockInputSafeTool{staticSafe: true}
	assert.False(t, tool.IsConcurrencySafeForInput(json.RawMessage(`{}`)))
}
