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
		"PROMPT IS TOO LONG: exceeded",
		"Context_Length_Exceeded",
		"HTTP 413 Payload Too Large",
		"error 413: request entity too large",
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
		"overloaded",
		"OVERLOADED",
		"APIError 529: ",
		"error 529: service unavailable",
		"ServiceUnavailable 503",
		"error 503: backend unavailable",
		"HTTP 503 Service Unavailable",
		"insufficient capacity",
		"529 ",
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
		"MAX_OUTPUT_TOKENS: hit limit",
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
		"IMAGE TOO LARGE: exceeds 20MB",
		"File Too Large for upload",
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
	assert.Equal(t, ClassUnknown, Classify(errors.New("")))
}

func TestClassifyFalsePositives(t *testing.T) {
	cases := []struct {
		msg    string
		reason string
	}{
		{"error code 4130 is unknown", `"4130" should not match 413`},
		{"failed to load config from port 5032", `"5032" should not match 503`},
		{"error code 5290 is unknown", `"5290" should not match 529`},
		{"connection to port 5032 refused", `"5032" in port should not match`},
		{"phone number 14135551234", `"4135" should not match 413`},
		{"chunk 1413 of 5000 uploaded", `"1413" should not match 413`},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			assert.Equal(t, ClassUnknown, Classify(errors.New(tc.msg)), tc.reason)
		})
	}
}

func TestClassifyPriorityOrder(t *testing.T) {
	assert.Equal(t, ClassPromptTooLong, Classify(errors.New("error 413: file too large")),
		"prompt_too_long checked before media_size")
	assert.Equal(t, ClassMaxOutputTokens, Classify(errors.New("max_output_tokens error: overloaded")),
		"max_output_tokens checked before model_overloaded")
}

func TestClassifyIsRetryable(t *testing.T) {
	assert.True(t, IsRetryable(ClassModelOverloaded))
	assert.False(t, IsRetryable(ClassPromptTooLong))
	assert.False(t, IsRetryable(ClassUnknown))
	assert.False(t, IsRetryable(ClassMaxOutputTokens))
	assert.False(t, IsRetryable(ClassMediaSize))
}

func TestErrorClassString(t *testing.T) {
	assert.Equal(t, "unknown", ClassUnknown.String())
	assert.Equal(t, "prompt_too_long", ClassPromptTooLong.String())
	assert.Equal(t, "model_overloaded", ClassModelOverloaded.String())
	assert.Equal(t, "max_output_tokens", ClassMaxOutputTokens.String())
	assert.Equal(t, "media_size", ClassMediaSize.String())
}
