package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simpleScratchpad implements ScratchpadAccess for testing.
type simpleScratchpad struct {
	notes map[string]string
}

func newSimpleScratchpad() *simpleScratchpad {
	return &simpleScratchpad{notes: make(map[string]string)}
}

func (s *simpleScratchpad) Set(tag, content string) { s.notes[tag] = content }
func (s *simpleScratchpad) Get(tag string) string   { return s.notes[tag] }
func (s *simpleScratchpad) Delete(tag string)       { delete(s.notes, tag) }
func (s *simpleScratchpad) All() map[string]string {
	cp := make(map[string]string, len(s.notes))
	for k, v := range s.notes {
		cp[k] = v
	}
	return cp
}

func TestNotesToolName(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())
	assert.Equal(t, "notes", n.Name())
}

func TestNotesToolDescription(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())
	assert.NotEmpty(t, n.Description())
}

func TestNotesToolInputSchema(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())
	var schema map[string]interface{}
	err := json.Unmarshal(n.InputSchema(), &schema)
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])
}

func TestNotesToolSet(t *testing.T) {
	sp := newSimpleScratchpad()
	n := NewNotesTool(sp)

	input, _ := json.Marshal(notesInput{Action: "set", Tag: "plan", Content: "build feature X"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "saved")
	assert.Equal(t, "build feature X", sp.Get("plan"))
}

func TestNotesToolGet(t *testing.T) {
	sp := newSimpleScratchpad()
	sp.Set("plan", "build feature X")
	n := NewNotesTool(sp)

	input, _ := json.Marshal(notesInput{Action: "get", Tag: "plan"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "build feature X", result.Content)
}

func TestNotesToolGetMissing(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())

	input, _ := json.Marshal(notesInput{Action: "get", Tag: "missing"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "No note found")
}

func TestNotesToolDelete(t *testing.T) {
	sp := newSimpleScratchpad()
	sp.Set("temp", "data")
	n := NewNotesTool(sp)

	input, _ := json.Marshal(notesInput{Action: "delete", Tag: "temp"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "deleted")
	assert.Equal(t, "", sp.Get("temp"))
}

func TestNotesToolList(t *testing.T) {
	sp := newSimpleScratchpad()
	sp.Set("a", "alpha")
	sp.Set("b", "beta")
	n := NewNotesTool(sp)

	input, _ := json.Marshal(notesInput{Action: "list"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "a: alpha")
	assert.Contains(t, result.Content, "b: beta")
}

func TestNotesToolListEmpty(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())

	input, _ := json.Marshal(notesInput{Action: "list"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.Contains(t, result.Content, "No notes stored")
}

func TestNotesToolUnknownAction(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())

	input, _ := json.Marshal(notesInput{Action: "invalid"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown action")
}

func TestNotesToolSetMissingTag(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())

	input, _ := json.Marshal(notesInput{Action: "set", Content: "data"})
	result, err := n.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "tag is required")
}

func TestNotesToolInvalidJSON(t *testing.T) {
	n := NewNotesTool(newSimpleScratchpad())

	result, err := n.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}
