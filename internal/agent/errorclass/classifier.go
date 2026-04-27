package errorclass

import "strings"

type ErrorClass int

const (
	ClassUnknown ErrorClass = iota
	ClassPromptTooLong
	ClassModelOverloaded
	ClassMaxOutputTokens
	ClassMediaSize
)

func Classify(err error) ErrorClass {
	if err == nil {
		return ClassUnknown
	}
	msg := err.Error()
	msgLower := strings.ToLower(msg)

	if isPromptTooLong(msg, msgLower) {
		return ClassPromptTooLong
	}
	if isMaxOutputTokens(msg, msgLower) {
		return ClassMaxOutputTokens
	}
	if isMediaSize(msgLower) {
		return ClassMediaSize
	}
	if isModelOverloaded(msg, msgLower) {
		return ClassModelOverloaded
	}
	return ClassUnknown
}

func IsRetryable(class ErrorClass) bool {
	return class == ClassModelOverloaded
}

func isPromptTooLong(msg, msgLower string) bool {
	return strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "413") ||
		strings.Contains(msgLower, "too many tokens")
}

func isModelOverloaded(msg, msgLower string) bool {
	return strings.Contains(msg, "Overloaded") ||
		strings.Contains(msg, "529") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msgLower, "capacity")
}

func isMaxOutputTokens(msg, msgLower string) bool {
	return strings.Contains(msg, "max_output_tokens") ||
		strings.Contains(msgLower, "max output tokens exceeded")
}

func isMediaSize(msgLower string) bool {
	return strings.Contains(msgLower, "media_size_error") ||
		strings.Contains(msgLower, "image too large") ||
		strings.Contains(msgLower, "file too large")
}
