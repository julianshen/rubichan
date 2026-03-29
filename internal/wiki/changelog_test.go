package wiki

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------- test helpers ----------

type mockLLM struct {
	response string
	err      error
	calls    int
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	m.calls++
	return m.response, m.err
}

// ---------- TestExtractChangelog ----------

func TestExtractChangelog(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantBody      string
		wantChangelog string
	}{
		{
			name:          "no changelog section",
			content:       "# Title\n\nSome content here.",
			wantBody:      "# Title\n\nSome content here.",
			wantChangelog: "",
		},
		{
			name:          "with changelog section",
			content:       "# Title\n\nBody text.\n\n## Change History\n\n- **2026-01-01** — Initial generation",
			wantBody:      "# Title\n\nBody text.\n\n",
			wantChangelog: "## Change History\n\n- **2026-01-01** — Initial generation",
		},
		{
			name:          "changelog at start",
			content:       "## Change History\n\n- **2026-01-01** — Initial generation",
			wantBody:      "",
			wantChangelog: "## Change History\n\n- **2026-01-01** — Initial generation",
		},
		{
			name:          "empty content",
			content:       "",
			wantBody:      "",
			wantChangelog: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBody, gotChangelog := extractChangelog(tt.content)
			if gotBody != tt.wantBody {
				t.Errorf("body: got %q, want %q", gotBody, tt.wantBody)
			}
			if gotChangelog != tt.wantChangelog {
				t.Errorf("changelog: got %q, want %q", gotChangelog, tt.wantChangelog)
			}
		})
	}
}

// ---------- TestApplyChangelog_NewDoc ----------

func TestApplyChangelog_NewDoc(t *testing.T) {
	ctx := context.Background()
	llm := &mockLLM{response: "Added new feature"}
	existing := map[string]string{} // nothing pre-existing

	newDocs := []Document{
		{Path: "docs/index.md", Title: "Index", Content: "# Index\n\nHello world."},
	}

	result, err := ApplyChangelog(ctx, existing, newDocs, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result))
	}

	today := time.Now().UTC().Format("2006-01-02")
	content := result[0].Content

	if !strings.Contains(content, "## Change History") {
		t.Error("expected ## Change History section")
	}
	if !strings.Contains(content, "Initial generation") {
		t.Error("expected 'Initial generation' entry")
	}
	if !strings.Contains(content, today) {
		t.Errorf("expected today's date %s in changelog", today)
	}
	// LLM should NOT be called for new docs
	if llm.calls != 0 {
		t.Errorf("expected 0 LLM calls for new doc, got %d", llm.calls)
	}
}

// ---------- TestApplyChangelog_UnchangedDoc ----------

func TestApplyChangelog_UnchangedDoc(t *testing.T) {
	ctx := context.Background()
	llm := &mockLLM{response: "Something changed"}

	body := "# Index\n\nHello world.\n\n"
	changelog := "## Change History\n\n- **2026-01-01** — Initial generation"
	existingContent := body + changelog

	existing := map[string]string{
		"docs/index.md": existingContent,
	}
	newDocs := []Document{
		// same body content (no changelog)
		{Path: "docs/index.md", Title: "Index", Content: "# Index\n\nHello world."},
	}

	result, err := ApplyChangelog(ctx, existing, newDocs, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result))
	}

	// Content should be preserved as-is (existing content returned)
	if result[0].Content != existingContent {
		t.Errorf("expected preserved content, got %q", result[0].Content)
	}
	// LLM should NOT be called for unchanged docs
	if llm.calls != 0 {
		t.Errorf("expected 0 LLM calls for unchanged doc, got %d", llm.calls)
	}
}

// ---------- TestApplyChangelog_ChangedDoc ----------

func TestApplyChangelog_ChangedDoc(t *testing.T) {
	ctx := context.Background()
	llm := &mockLLM{response: "Refactored the overview section"}

	body := "# Index\n\nOld content.\n\n"
	changelog := "## Change History\n\n- **2026-01-01** — Initial generation"
	existingContent := body + changelog

	existing := map[string]string{
		"docs/index.md": existingContent,
	}
	newDocs := []Document{
		{Path: "docs/index.md", Title: "Index", Content: "# Index\n\nNew content."},
	}

	result, err := ApplyChangelog(ctx, existing, newDocs, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result))
	}

	today := time.Now().UTC().Format("2006-01-02")
	content := result[0].Content

	if !strings.Contains(content, "## Change History") {
		t.Error("expected ## Change History section")
	}
	if !strings.Contains(content, today) {
		t.Errorf("expected today %s in changelog", today)
	}
	if !strings.Contains(content, "Refactored the overview section") {
		t.Error("expected LLM summary in changelog entry")
	}
	// LLM should be called once
	if llm.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.calls)
	}
}

// ---------- TestApplyChangelog_PreservesExistingHistory ----------

func TestApplyChangelog_PreservesExistingHistory(t *testing.T) {
	ctx := context.Background()
	llm := &mockLLM{response: "Updated API docs"}

	oldEntry := "- **2026-01-01** — Initial generation"
	body := "# API Docs\n\nOld API content.\n\n"
	changelog := "## Change History\n\n" + oldEntry
	existingContent := body + changelog

	existing := map[string]string{
		"docs/api.md": existingContent,
	}
	newDocs := []Document{
		{Path: "docs/api.md", Title: "API Docs", Content: "# API Docs\n\nNew API content."},
	}

	result, err := ApplyChangelog(ctx, existing, newDocs, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := result[0].Content
	// Old entry must still be present
	if !strings.Contains(content, oldEntry) {
		t.Error("expected old changelog entry to be preserved")
	}
	// New entry must also appear
	if !strings.Contains(content, "Updated API docs") {
		t.Error("expected new LLM summary in changelog")
	}
}

// ---------- TestApplyChangelog_MaxEntries ----------

func TestApplyChangelog_MaxEntries(t *testing.T) {
	ctx := context.Background()
	llm := &mockLLM{response: "Latest update"}

	// Build a changelog with 50 entries already
	var lines []string
	lines = append(lines, "## Change History", "")
	for i := 50; i >= 1; i-- {
		lines = append(lines, strings.Repeat(" ", 0)+
			"- **2025-01-"+zeroPad(i)+"** — Entry number "+zeroPad(i))
	}
	existingContent := "# Doc\n\nOld body.\n\n" + strings.Join(lines, "\n")

	existing := map[string]string{"docs/big.md": existingContent}
	newDocs := []Document{
		{Path: "docs/big.md", Title: "Doc", Content: "# Doc\n\nNew body."},
	}

	result, err := ApplyChangelog(ctx, existing, newDocs, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := result[0].Content
	_, cl := extractChangelog(content)
	entryCount := countChangelogEntries(cl)
	if entryCount > 50 {
		t.Errorf("expected at most 50 entries, got %d", entryCount)
	}
}

// zeroPad pads a small int to two digits (01..50).
func zeroPad(n int) string {
	if n < 10 {
		return "0" + strings.TrimSpace(strings.Repeat("0", 1)) + string(rune('0'+n))
	}
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}

// countChangelogEntries counts lines starting with "- **" in the changelog.
func countChangelogEntries(changelog string) int {
	count := 0
	for _, line := range strings.Split(changelog, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- **") {
			count++
		}
	}
	return count
}

// ---------- TestApplyChangelog_LLMFallback ----------

func TestApplyChangelog_LLMFallback(t *testing.T) {
	ctx := context.Background()
	llm := &mockLLM{err: errors.New("LLM unavailable")}

	existing := map[string]string{
		"docs/index.md": "# Index\n\nOld body.\n\n## Change History\n\n- **2026-01-01** — Initial generation",
	}
	newDocs := []Document{
		{Path: "docs/index.md", Title: "Index", Content: "# Index\n\nNew body."},
	}

	result, err := ApplyChangelog(ctx, existing, newDocs, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result))
	}

	content := result[0].Content
	if !strings.Contains(content, "Content updated") {
		t.Errorf("expected fallback 'Content updated' entry, got:\n%s", content)
	}
}
