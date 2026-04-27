package errorclass

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyPromptTooLong(t *testing.T) {
	cases := []string{
		"prompt is too long: 204857 tokens > 200000 maximum",
		"Error: context_length_exceeded",
		"Request too large: 413",
		"too many tokens in request",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassPromptTooLong, Classify(errors.New(msg)))
		})
	}
}

func TestClassifyModelOverloaded(t *testing.T) {
	cases := []string{
		"Overloaded",
		"APIError 529: ",
		"ServiceUnavailable 503",
		"insufficient capacity",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassModelOverloaded, Classify(errors.New(msg)))
		})
	}
}

func TestClassifyMaxOutputTokens(t *testing.T) {
	cases := []string{
		"max_output_tokens exceeded",
		"Max output tokens exceeded: response was truncated",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassMaxOutputTokens, Classify(errors.New(msg)))
		})
	}
}

func TestClassifyMediaSize(t *testing.T) {
	cases := []string{
		"media_size_error: image too large",
		"file too large for processing",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassMediaSize, Classify(errors.New(msg)))
		})
	}
}

func TestClassifyUnknown(t *testing.T) {
	assert.Equal(t, ClassUnknown, Classify(errors.New("something unexpected")))
	assert.Equal(t, ClassUnknown, Classify(nil))
}

func TestClassifyIsRetryable(t *testing.T) {
	assert.True(t, IsRetryable(ClassModelOverloaded))
	assert.False(t, IsRetryable(ClassPromptTooLong))
	assert.False(t, IsRetryable(ClassUnknown))
}
