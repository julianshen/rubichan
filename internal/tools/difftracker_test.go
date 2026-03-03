package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDiffTracker(t *testing.T) {
	dt := NewDiffTracker()
	require.NotNil(t, dt)
	assert.Empty(t, dt.Changes())
}

func TestDiffTrackerRecordCreated(t *testing.T) {
	dt := NewDiffTracker()

	dt.Record(FileChange{
		Path:      "new_file.go",
		Operation: OpCreated,
		Diff:      "+package main\n+func main() {}",
		Tool:      "file",
	})

	changes := dt.Changes()
	require.Len(t, changes, 1)
	assert.Equal(t, "new_file.go", changes[0].Path)
	assert.Equal(t, OpCreated, changes[0].Operation)
	assert.Equal(t, "file", changes[0].Tool)
	assert.Contains(t, changes[0].Diff, "+package main")
}

func TestDiffTrackerRecordModified(t *testing.T) {
	dt := NewDiffTracker()

	dt.Record(FileChange{
		Path:      "existing.go",
		Operation: OpModified,
		Diff:      "-old line\n+new line",
		Tool:      "file",
	})

	changes := dt.Changes()
	require.Len(t, changes, 1)
	assert.Equal(t, OpModified, changes[0].Operation)
}

func TestDiffTrackerRecordDeleted(t *testing.T) {
	dt := NewDiffTracker()

	dt.Record(FileChange{
		Path:      "removed.go",
		Operation: OpDeleted,
		Diff:      "-package removed",
		Tool:      "shell",
	})

	changes := dt.Changes()
	require.Len(t, changes, 1)
	assert.Equal(t, OpDeleted, changes[0].Operation)
	assert.Equal(t, "shell", changes[0].Tool)
}

func TestDiffTrackerMultipleChanges(t *testing.T) {
	dt := NewDiffTracker()

	dt.Record(FileChange{Path: "a.go", Operation: OpCreated, Tool: "file"})
	dt.Record(FileChange{Path: "b.go", Operation: OpModified, Tool: "file"})
	dt.Record(FileChange{Path: "c.go", Operation: OpDeleted, Tool: "shell"})

	changes := dt.Changes()
	require.Len(t, changes, 3)
	assert.Equal(t, "a.go", changes[0].Path)
	assert.Equal(t, "b.go", changes[1].Path)
	assert.Equal(t, "c.go", changes[2].Path)
}

func TestDiffTrackerChangesReturnsCopy(t *testing.T) {
	dt := NewDiffTracker()
	dt.Record(FileChange{Path: "a.go", Operation: OpCreated, Tool: "file"})

	changes1 := dt.Changes()
	changes2 := dt.Changes()
	// Modifying returned slice should not affect tracker
	changes1[0].Path = "modified.go"
	assert.Equal(t, "a.go", changes2[0].Path)
}

func TestDiffTrackerReset(t *testing.T) {
	dt := NewDiffTracker()
	dt.Record(FileChange{Path: "a.go", Operation: OpCreated, Tool: "file"})
	dt.Record(FileChange{Path: "b.go", Operation: OpModified, Tool: "shell"})

	require.Len(t, dt.Changes(), 2)

	dt.Reset()
	assert.Empty(t, dt.Changes())
}

func TestDiffTrackerSummarizeEmpty(t *testing.T) {
	dt := NewDiffTracker()
	summary := dt.Summarize()
	assert.Equal(t, "", summary)
}

func TestDiffTrackerSummarizeSingleFile(t *testing.T) {
	dt := NewDiffTracker()
	dt.Record(FileChange{
		Path:      "main.go",
		Operation: OpCreated,
		Diff:      "+package main",
		Tool:      "file",
	})

	summary := dt.Summarize()
	assert.Contains(t, summary, "main.go")
	assert.Contains(t, summary, "created")
}

func TestDiffTrackerSummarizeMultipleFiles(t *testing.T) {
	dt := NewDiffTracker()
	dt.Record(FileChange{Path: "a.go", Operation: OpCreated, Tool: "file"})
	dt.Record(FileChange{Path: "b.go", Operation: OpModified, Diff: "-old\n+new", Tool: "file"})
	dt.Record(FileChange{Path: "c.go", Operation: OpDeleted, Tool: "shell"})

	summary := dt.Summarize()
	assert.Contains(t, summary, "a.go")
	assert.Contains(t, summary, "b.go")
	assert.Contains(t, summary, "c.go")
	assert.Contains(t, summary, "created")
	assert.Contains(t, summary, "modified")
	assert.Contains(t, summary, "deleted")
}

func TestDiffTrackerSummarizeIncludesDiff(t *testing.T) {
	dt := NewDiffTracker()
	dt.Record(FileChange{
		Path:      "main.go",
		Operation: OpModified,
		Diff:      "-old line\n+new line",
		Tool:      "file",
	})

	summary := dt.Summarize()
	assert.Contains(t, summary, "-old line")
	assert.Contains(t, summary, "+new line")
}

func TestDiffTrackerConcurrentAccess(t *testing.T) {
	dt := NewDiffTracker()
	done := make(chan struct{})

	// Write from one goroutine
	go func() {
		for i := 0; i < 100; i++ {
			dt.Record(FileChange{Path: "test.go", Operation: OpModified, Tool: "file"})
		}
		close(done)
	}()

	// Read from another goroutine
	for i := 0; i < 100; i++ {
		_ = dt.Changes()
		_ = dt.Summarize()
	}

	<-done
	assert.GreaterOrEqual(t, len(dt.Changes()), 1)
}

func TestDiffTrackerSummarizeCountsByOperation(t *testing.T) {
	dt := NewDiffTracker()
	dt.Record(FileChange{Path: "a.go", Operation: OpCreated, Tool: "file"})
	dt.Record(FileChange{Path: "b.go", Operation: OpCreated, Tool: "file"})
	dt.Record(FileChange{Path: "c.go", Operation: OpModified, Tool: "shell"})

	summary := dt.Summarize()
	// Summary header should mention counts
	assert.Contains(t, summary, "3 file(s) changed")
}

func TestOperationString(t *testing.T) {
	assert.Equal(t, "created", OpCreated.String())
	assert.Equal(t, "modified", OpModified.String())
	assert.Equal(t, "deleted", OpDeleted.String())
}
