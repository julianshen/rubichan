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

	last := b.LastUnrecovered()
	require.NotNil(t, last)
	assert.Equal(t, errorclass.ClassMaxOutputTokens, last.Class)
}

func TestWithheldErrorBuffer_Clear(t *testing.T) {
	b := &withheldErrorBuffer{}
	b.Add(errorclass.ClassPromptTooLong, fmt.Errorf("err"))
	b.Clear()
	assert.False(t, b.HasUnrecovered())
}
