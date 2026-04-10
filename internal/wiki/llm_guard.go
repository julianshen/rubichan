package wiki

import (
	"context"
	"strings"
	"unicode"
)

// isResponseTruncated detects if an LLM response appears to have been cut off
// mid-generation. It checks for unclosed fenced code blocks, continuation
// punctuation at the end, and lines ending abruptly without terminal punctuation.
func isResponseTruncated(s string) bool {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return false
	}

	// Unclosed fenced code block.
	if strings.Count(s, "```")%2 != 0 {
		return true
	}

	// Get last non-empty line.
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			last = trimmed
			break
		}
	}
	if last == "" {
		return false
	}

	lastRune := rune(last[len(last)-1])

	// Ends with continuation punctuation.
	if lastRune == ':' || lastRune == ',' || lastRune == ';' {
		return true
	}

	// Terminal punctuation or structural markers mean complete.
	isTerminal := lastRune == '.' || lastRune == '!' || lastRune == '?' ||
		lastRune == '|' || lastRune == '*' || lastRune == '`' || lastRune == '-'
	isStructural := strings.HasPrefix(last, "#") || strings.HasPrefix(last, "-") ||
		strings.HasPrefix(last, "*") || strings.HasPrefix(last, "|") ||
		strings.HasPrefix(last, "```")

	if !isTerminal && !isStructural {
		if unicode.IsLetter(lastRune) || unicode.IsDigit(lastRune) || lastRune == ')' {
			return true
		}
	}

	return false
}

// completeLLMResponse calls the LLM and, if the response appears truncated,
// retries up to maxRetries times with a continuation prompt. The continuation
// text is appended to the original response.
func completeLLMResponse(ctx context.Context, prompt string, llm LLMCompleter, maxRetries int) (string, error) {
	resp, err := llm.Complete(ctx, prompt)
	if err != nil {
		return "", err
	}

	for i := 0; i < maxRetries && isResponseTruncated(resp); i++ {
		tail := resp
		runes := []rune(tail)
		if len(runes) > 200 {
			tail = string(runes[len(runes)-200:])
		}
		contPrompt := "Your previous response was cut off. Continue from where you left off. Your previous response ended with:\n\n..." + tail
		cont, err := llm.Complete(ctx, contPrompt)
		if err != nil {
			break // Return what we have.
		}
		resp += cont
	}

	return resp, nil
}
