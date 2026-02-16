package pipeline

import "fmt"

// BuildReviewPrompt constructs a code review prompt from a git diff.
func BuildReviewPrompt(diff string) string {
	if diff == "" {
		return "There are no changes to review (no diff found, no changes detected)."
	}

	return fmt.Sprintf(`You are reviewing the following code changes. Analyze the diff and provide:
1. A brief summary of what changed
2. Issues found (bugs, security, performance, style) with severity (high/medium/low)
3. Suggestions for improvement

If there are no issues, say so.

<diff>
%s
</diff>`, diff)
}
