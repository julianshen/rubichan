package platform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/platform"
	"github.com/julianshen/rubichan/internal/security"
	secoutput "github.com/julianshen/rubichan/internal/security/output"
)

// mockPlatform records calls for verification.
type mockPlatform struct {
	name           string
	comments       []string
	reviews        []platform.Review
	sarifUploads   [][]byte
	commentErr     error
	reviewErr      error
	sarifUploadErr error
}

func (m *mockPlatform) Name() string { return m.name }
func (m *mockPlatform) PostPRComment(_ context.Context, _ string, _ int, body string) error {
	m.comments = append(m.comments, body)
	return m.commentErr
}
func (m *mockPlatform) PostPRReview(_ context.Context, _ string, _ int, review platform.Review) error {
	m.reviews = append(m.reviews, review)
	return m.reviewErr
}
func (m *mockPlatform) GetPRDiff(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (m *mockPlatform) ListPRFiles(_ context.Context, _ string, _ int) ([]platform.PRFile, error) {
	return nil, nil
}
func (m *mockPlatform) UploadSARIF(_ context.Context, _ string, _ string, data []byte) error {
	m.sarifUploads = append(m.sarifUploads, data)
	return m.sarifUploadErr
}

func TestBridgePostReviewFromSecurityReport(t *testing.T) {
	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:       "F1",
				Title:    "SQL injection",
				Severity: security.SeverityHigh,
				Location: security.Location{File: "db.go", StartLine: 42},
			},
		},
	}

	mock := &mockPlatform{name: "github"}
	formatter := secoutput.NewGitHubPRFormatter()
	err := platform.PostSecurityReview(context.Background(), mock, formatter, report, "o/r", 1)
	if err != nil {
		t.Fatalf("PostSecurityReview() error = %v", err)
	}
	if len(mock.reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(mock.reviews))
	}
	if len(mock.reviews[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(mock.reviews[0].Comments))
	}
	if mock.reviews[0].Comments[0].Path != "db.go" {
		t.Errorf("comment path = %q, want %q", mock.reviews[0].Comments[0].Path, "db.go")
	}
}

func TestBridgePostReviewEmptyReport(t *testing.T) {
	report := &security.Report{}

	mock := &mockPlatform{name: "github"}
	formatter := secoutput.NewGitHubPRFormatter()
	err := platform.PostSecurityReview(context.Background(), mock, formatter, report, "o/r", 1)
	if err != nil {
		t.Fatalf("PostSecurityReview() error = %v", err)
	}
	// Empty report produces a review with summary body but no inline comments.
	if len(mock.reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(mock.reviews))
	}
	if len(mock.reviews[0].Comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(mock.reviews[0].Comments))
	}
}

func TestBridgePostSARIF(t *testing.T) {
	report := &security.Report{
		Findings: []security.Finding{
			{ID: "F1", Title: "test", CWE: "CWE-89", Severity: security.SeverityHigh, Location: security.Location{File: "a.go", StartLine: 1, EndLine: 1}},
		},
	}

	mock := &mockPlatform{name: "github"}
	formatter := secoutput.NewSARIFFormatter()
	err := platform.UploadSecuritySARIF(context.Background(), mock, formatter, report, "o/r", "abc123")
	if err != nil {
		t.Fatalf("UploadSecuritySARIF() error = %v", err)
	}
	if len(mock.sarifUploads) != 1 {
		t.Fatalf("expected 1 SARIF upload, got %d", len(mock.sarifUploads))
	}
	if !strings.Contains(string(mock.sarifUploads[0]), "2.1.0") {
		t.Error("SARIF output should contain version 2.1.0")
	}
}

func TestBridgePostRunResult(t *testing.T) {
	result := &output.RunResult{
		Response: "Code looks clean",
		Mode:     "code-review",
	}

	mock := &mockPlatform{name: "github"}
	formatter := output.NewPRCommentFormatter()
	err := platform.PostRunResultComment(context.Background(), mock, formatter, result, "o/r", 1)
	if err != nil {
		t.Fatalf("PostRunResultComment() error = %v", err)
	}
	if len(mock.comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(mock.comments))
	}
	if !strings.Contains(mock.comments[0], "Code looks clean") {
		t.Error("comment should contain the response")
	}
}

func TestBridgeTruncatesLongComments(t *testing.T) {
	result := &output.RunResult{
		Response: strings.Repeat("x", 70000),
		Mode:     "generic",
	}

	mock := &mockPlatform{name: "github"}
	formatter := output.NewPRCommentFormatter()
	err := platform.PostRunResultComment(context.Background(), mock, formatter, result, "o/r", 1)
	if err != nil {
		t.Fatalf("PostRunResultComment() error = %v", err)
	}
	if len(mock.comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(mock.comments))
	}
	if len(mock.comments[0]) > 65100 {
		t.Errorf("comment length = %d, want <= ~65000", len(mock.comments[0]))
	}
	if !strings.Contains(mock.comments[0], "truncated") {
		t.Error("truncated comment should contain truncation notice")
	}
}

func TestBridgeMapsSecurityPRCommentToPlatformReview(t *testing.T) {
	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:       "F1",
				Title:    "Issue",
				Severity: security.SeverityMedium,
				Location: security.Location{File: "api.go", StartLine: 25},
			},
		},
	}

	mock := &mockPlatform{name: "github"}
	formatter := secoutput.NewGitHubPRFormatter()
	_ = platform.PostSecurityReview(context.Background(), mock, formatter, report, "o/r", 1)

	if len(mock.reviews) != 1 || len(mock.reviews[0].Comments) != 1 {
		t.Fatal("expected 1 review with 1 comment")
	}
	c := mock.reviews[0].Comments[0]
	if c.Path != "api.go" || c.Line != 25 || c.Side != "RIGHT" {
		t.Errorf("comment = %+v, want path=api.go line=25 side=RIGHT", c)
	}
}

// TestBridgeAcceptsAnyOutputFormatter verifies the bridge works with any
// security.OutputFormatter, not just GitHubPRFormatter.
func TestBridgeAcceptsAnyOutputFormatter(t *testing.T) {
	report := &security.Report{
		Findings: []security.Finding{
			{ID: "F1", Title: "test", Severity: security.SeverityLow},
		},
	}

	// Use MarkdownFormatter instead of GitHubPRFormatter.
	// It produces markdown, not PRReview JSON, so the bridge should
	// fall back to posting as a plain comment.
	mock := &mockPlatform{name: "github"}
	formatter := secoutput.NewMarkdownFormatter()
	err := platform.PostSecurityReview(context.Background(), mock, formatter, report, "o/r", 1)
	if err != nil {
		t.Fatalf("PostSecurityReview() error = %v", err)
	}
	// Should have fallen back to a plain comment since markdown isn't PRReview JSON.
	if len(mock.comments) != 1 {
		t.Errorf("expected 1 comment (fallback), got %d comments and %d reviews", len(mock.comments), len(mock.reviews))
	}
}

// TestBridgeAcceptsAnyFormatter verifies PostRunResultComment works with
// any output.Formatter implementation.
func TestBridgeAcceptsAnyFormatter(t *testing.T) {
	result := &output.RunResult{
		Response: "test response",
		Mode:     "generic",
	}

	mock := &mockPlatform{name: "github"}
	// Use MarkdownFormatter instead of PRCommentFormatter.
	formatter := output.NewMarkdownFormatter()
	err := platform.PostRunResultComment(context.Background(), mock, formatter, result, "o/r", 1)
	if err != nil {
		t.Fatalf("PostRunResultComment() error = %v", err)
	}
	if len(mock.comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(mock.comments))
	}
}
