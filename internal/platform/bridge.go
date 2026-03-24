package platform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/security"
	secoutput "github.com/julianshen/rubichan/internal/security/output"
)

const maxCommentLength = 65000

// PostSecurityReview formats a security report via the given OutputFormatter
// and posts it as a PR review with inline comments.
func PostSecurityReview(
	ctx context.Context,
	p Platform,
	formatter security.OutputFormatter,
	report *security.Report,
	repo string,
	prNumber int,
) error {
	formatted, err := formatter.Format(report)
	if err != nil {
		return fmt.Errorf("formatting security review: %w", err)
	}

	var prReview secoutput.PRReview
	if err := json.Unmarshal(formatted, &prReview); err != nil {
		// Formatter didn't produce PRReview JSON — post as plain comment.
		body := truncate(string(formatted))
		return p.PostPRComment(ctx, repo, prNumber, body)
	}

	review := Review{
		Body:  prReview.Body,
		Event: "COMMENT",
	}
	for _, c := range prReview.Comments {
		if c.Path == "" || c.Line <= 0 {
			continue
		}
		review.Comments = append(review.Comments, ReviewComment{
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
			Side: "RIGHT",
		})
	}

	return p.PostPRReview(ctx, repo, prNumber, review)
}

// UploadSecuritySARIF formats a security report as SARIF and uploads it.
func UploadSecuritySARIF(
	ctx context.Context,
	p Platform,
	formatter security.OutputFormatter,
	report *security.Report,
	repo string,
	ref string,
) error {
	sarifBytes, err := formatter.Format(report)
	if err != nil {
		return fmt.Errorf("formatting SARIF: %w", err)
	}
	return p.UploadSARIF(ctx, repo, ref, sarifBytes)
}

// PostRunResultComment formats a RunResult and posts it as a PR comment.
func PostRunResultComment(
	ctx context.Context,
	p Platform,
	formatter output.Formatter,
	result *output.RunResult,
	repo string,
	prNumber int,
) error {
	body, err := formatter.Format(result)
	if err != nil {
		return fmt.Errorf("formatting run result: %w", err)
	}
	return p.PostPRComment(ctx, repo, prNumber, truncate(string(body)))
}

// truncate shortens text to fit within GitHub's comment size limit.
func truncate(s string) string {
	if len(s) <= maxCommentLength {
		return s
	}
	const notice = "\n\n---\n*Output truncated. Full report available in CI logs.*\n"
	return s[:maxCommentLength-len(notice)] + notice
}
