package agent

import (
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/agent/errorclass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithheldErrorBuffer(t *testing.T) {
	b := &withheldErrorBuffer{}
	b.Add(errorclass.ClassPromptTooLong, fmt.Errorf("prompt too long"))
	assert.True(t, b.HasUnrecovered())

	b.MarkRecovered(errorclass.ClassPromptTooLong)
	assert.False(t, b.HasUnrecovered())
}

func TestWithheldErrorBuffer_LastUnrecovered(t *testing.T) {
	b := &withheldErrorBuffer{}
	b.Add(errorclass.ClassPromptTooLong, fmt.Errorf("first"))
	b.Add(errorclass.ClassMaxOutputTokens, fmt.Errorf("second"))

	last, ok := b.LastUnrecovered()
	require.True(t, ok)
	assert.Equal(t, errorclass.ClassMaxOutputTokens, last.Class)
}

func TestWithheldErrorBuffer_Clear(t *testing.T) {
	b := &withheldErrorBuffer{}
	b.Add(errorclass.ClassPromptTooLong, fmt.Errorf("err"))
	b.Clear()
	assert.False(t, b.HasUnrecovered())
}

func TestWithheldErrorBuffer_MarkRecovered_WrongClass(t *testing.T) {
	b := &withheldErrorBuffer{}
	b.Add(errorclass.ClassPromptTooLong, fmt.Errorf("err"))
	b.MarkRecovered(errorclass.ClassMaxOutputTokens)
	assert.True(t, b.HasUnrecovered(), "MarkRecovered with wrong class should not clear the error")
}

func TestWithheldErrorBuffer_LastUnrecovered_Empty(t *testing.T) {
	b := &withheldErrorBuffer{}
	_, ok := b.LastUnrecovered()
	assert.False(t, ok, "empty buffer should return false")
}

func TestWithheldErrorBuffer_LastUnrecovered_AfterRecovered(t *testing.T) {
	b := &withheldErrorBuffer{}
	b.Add(errorclass.ClassPromptTooLong, fmt.Errorf("err"))
	b.MarkRecovered(errorclass.ClassPromptTooLong)
	_, ok := b.LastUnrecovered()
	assert.False(t, ok, "recovered error should return false")
}
