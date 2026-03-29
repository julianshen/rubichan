package wiki

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	changelogMarker     = "## Change History"
	maxChangelogEntries = 50
	maxLinesForDiff     = 500
)

// ApplyChangelog compares each new document against existing content and
// attaches an updated Change History section. New docs get an "Initial
// generation" entry; changed docs get an LLM-summarized entry; unchanged docs
// are returned as-is. LLM summary calls run concurrently.
func ApplyChangelog(ctx context.Context, existing map[string]string, newDocs []Document, llm LLMCompleter) ([]Document, error) {
	today := time.Now().UTC().Format("2006-01-02")

	type work struct {
		idx     int
		oldBody string
		newBody string
		oldCL   string
	}

	results := make([]Document, len(newDocs))
	var pending []work

	// First pass: handle new and unchanged docs synchronously; collect changed docs.
	for i, doc := range newDocs {
		newBody, _ := extractChangelog(doc.Content)

		existingContent, exists := existing[doc.Path]
		if !exists {
			// New document — attach initial generation entry.
			cl := appendChangelogEntry("", today, "Initial generation")
			results[i] = Document{
				Path:    doc.Path,
				Title:   doc.Title,
				Content: newBody + cl,
			}
			continue
		}

		oldBody, oldCL := extractChangelog(existingContent)
		// Normalise trailing whitespace for body comparison.
		if strings.TrimSpace(oldBody) == strings.TrimSpace(newBody) {
			// Unchanged — return original content verbatim.
			results[i] = Document{
				Path:    doc.Path,
				Title:   doc.Title,
				Content: existingContent,
			}
			continue
		}

		// Changed — queue for concurrent LLM summary.
		pending = append(pending, work{
			idx:     i,
			oldBody: oldBody,
			newBody: newBody,
			oldCL:   oldCL,
		})
	}

	if len(pending) == 0 {
		return results, nil
	}

	// Second pass: concurrent LLM summarization for changed docs.
	type result struct {
		idx     int
		summary string
	}
	summaries := make([]result, len(pending))

	var wg sync.WaitGroup
	for j, w := range pending {
		j, w := j, w
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ctx.Err() != nil {
				summaries[j] = result{idx: w.idx, summary: "Content updated"}
				return
			}
			prompt := buildChangelogPrompt(w.oldBody, w.newBody)
			summary, err := llm.Complete(ctx, prompt)
			if err != nil || strings.TrimSpace(summary) == "" {
				summary = "Content updated"
			}
			summaries[j] = result{idx: w.idx, summary: strings.TrimSpace(summary)}
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Assemble results for changed docs.
	for j, s := range summaries {
		w := pending[j]
		doc := newDocs[s.idx]
		newBody, _ := extractChangelog(doc.Content)

		cl := appendChangelogEntry(w.oldCL, today, s.summary)
		cl = trimChangelogEntries(cl, maxChangelogEntries)

		results[s.idx] = Document{
			Path:    doc.Path,
			Title:   doc.Title,
			Content: newBody + cl,
		}
	}

	return results, nil
}

// extractChangelog splits content into the main body and the changelog section.
// The changelog section starts at the first line that equals "## Change History".
// The body includes everything up to (and including) the newline before the marker.
func extractChangelog(content string) (body, changelog string) {
	idx := strings.Index(content, changelogMarker)
	if idx < 0 {
		return content, ""
	}
	return content[:idx], content[idx:]
}

// appendChangelogEntry adds a new dated entry to an existing changelog string.
// If changelog is empty, it initialises the section header. The new entry is
// prepended so that the most recent change appears first.
func appendChangelogEntry(changelog, date, summary string) string {
	newEntry := fmt.Sprintf("- **%s** — %s", date, summary)

	if changelog == "" {
		return changelogMarker + "\n\n" + newEntry
	}

	// Strip the header to manipulate entries.
	body := strings.TrimPrefix(changelog, changelogMarker)
	body = strings.TrimPrefix(body, "\n\n")

	if body == "" {
		return changelogMarker + "\n\n" + newEntry
	}

	return changelogMarker + "\n\n" + newEntry + "\n" + body
}

// trimChangelogEntries removes the oldest entries so that at most maxEntries
// remain. Only lines that start with "- **" are counted as entries.
func trimChangelogEntries(changelog string, maxEntries int) string {
	header := changelogMarker + "\n\n"
	body := strings.TrimPrefix(changelog, header)

	lines := strings.Split(body, "\n")
	// Collect entry indices.
	var entryLines []int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- **") {
			entryLines = append(entryLines, i)
		}
	}

	if len(entryLines) <= maxEntries {
		return changelog
	}

	// Keep only the first maxEntries entries (most recent, since we prepend).
	keepUntil := entryLines[maxEntries] // index of the first entry to drop
	trimmed := strings.TrimRight(strings.Join(lines[:keepUntil], "\n"), "\n")
	return header + trimmed
}

// truncateToLines returns at most maxLines lines of s joined by newlines.
func truncateToLines(s string, maxLines int) string {
	lines := strings.SplitN(s, "\n", maxLines+1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

// buildChangelogPrompt constructs the LLM prompt for summarising a diff.
func buildChangelogPrompt(oldBody, newBody string) string {
	return fmt.Sprintf(
		"Given the old and new versions of a document, summarize what changed in one sentence.\n\nOld version:\n%s\n\nNew version:\n%s",
		truncateToLines(oldBody, maxLinesForDiff),
		truncateToLines(newBody, maxLinesForDiff),
	)
}
