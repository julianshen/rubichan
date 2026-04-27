package errorclass

import (
	"strings"
)

type ErrorClass int

const (
	ClassUnknown ErrorClass = iota
	ClassPromptTooLong
	ClassModelOverloaded
	ClassMaxOutputTokens
	ClassMediaSize
)

func (c ErrorClass) String() string {
	switch c {
	case ClassPromptTooLong:
		return "prompt_too_long"
	case ClassModelOverloaded:
		return "model_overloaded"
	case ClassMaxOutputTokens:
		return "max_output_tokens"
	case ClassMediaSize:
		return "media_size"
	default:
		return "unknown"
	}
}

func Classify(err error) ErrorClass {
	if err == nil {
		return ClassUnknown
	}
	msg := strings.ToLower(err.Error())

	if isPromptTooLong(msg) {
		return ClassPromptTooLong
	}
	if isMaxOutputTokens(msg) {
		return ClassMaxOutputTokens
	}
	if isMediaSize(msg) {
		return ClassMediaSize
	}
	if isModelOverloaded(msg) {
		return ClassModelOverloaded
	}
	return ClassUnknown
}

func IsRetryable(class ErrorClass) bool {
	return class == ClassModelOverloaded
}

func isPromptTooLong(msg string) bool {
	return strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "context_length_exceeded") ||
		containsStatusCode(msg, "413") ||
		strings.Contains(msg, "too many tokens")
}

func isModelOverloaded(msg string) bool {
	return strings.Contains(msg, "overloaded") ||
		containsStatusCode(msg, "529") ||
		containsStatusCode(msg, "503") ||
		strings.Contains(msg, "insufficient capacity")
}

func isMaxOutputTokens(msg string) bool {
	return strings.Contains(msg, "max_output_tokens") ||
		strings.Contains(msg, "max output tokens exceeded")
}

func isMediaSize(msg string) bool {
	return strings.Contains(msg, "media_size_error") ||
		strings.Contains(msg, "image too large") ||
		strings.Contains(msg, "file too large")
}

func containsStatusCode(msg, code string) bool {
	idx := strings.Index(msg, code)
	for idx >= 0 {
		before := idx == 0 || !isDigit(msg[idx-1])
		after := idx+len(code) >= len(msg) || !isDigit(msg[idx+len(code)])
		if before && after {
			return true
		}
		next := strings.Index(msg[idx+1:], code)
		if next < 0 {
			break
		}
		idx += 1 + next
	}
	return false
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
