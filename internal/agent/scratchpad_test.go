package agent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScratchpadSetAndGet(t *testing.T) {
	sp := NewScratchpad()
	sp.Set("plan", "implement feature X")
	assert.Equal(t, "implement feature X", sp.Get("plan"))
}

func TestScratchpadGetMissing(t *testing.T) {
	sp := NewScratchpad()
	assert.Equal(t, "", sp.Get("nonexistent"))
}

func TestScratchpadSetOverwrites(t *testing.T) {
	sp := NewScratchpad()
	sp.Set("status", "in progress")
	sp.Set("status", "done")
	assert.Equal(t, "done", sp.Get("status"))
}

func TestScratchpadDelete(t *testing.T) {
	sp := NewScratchpad()
	sp.Set("temp", "value")
	sp.Delete("temp")
	assert.Equal(t, "", sp.Get("temp"))
}

func TestScratchpadDeleteNonexistent(t *testing.T) {
	sp := NewScratchpad()
	sp.Delete("nope") // should not panic
}

func TestScratchpadAll(t *testing.T) {
	sp := NewScratchpad()
	sp.Set("a", "1")
	sp.Set("b", "2")

	all := sp.All()
	assert.Len(t, all, 2)
	assert.Equal(t, "1", all["a"])
	assert.Equal(t, "2", all["b"])

	// Returned map should be a copy
	all["c"] = "3"
	assert.Equal(t, "", sp.Get("c"))
}

func TestScratchpadRenderEmpty(t *testing.T) {
	sp := NewScratchpad()
	assert.Equal(t, "", sp.Render())
}

func TestScratchpadRenderFormat(t *testing.T) {
	sp := NewScratchpad()
	sp.Set("architecture", "microservices pattern")
	sp.Set("decision", "use PostgreSQL")

	rendered := sp.Render()
	assert.Contains(t, rendered, "## Agent Notes")
	assert.Contains(t, rendered, "### architecture")
	assert.Contains(t, rendered, "microservices pattern")
	assert.Contains(t, rendered, "### decision")
	assert.Contains(t, rendered, "use PostgreSQL")
}

func TestScratchpadRenderSortedTags(t *testing.T) {
	sp := NewScratchpad()
	sp.Set("zebra", "z")
	sp.Set("alpha", "a")

	rendered := sp.Render()
	alphaIdx := len("## Agent Notes\n\n")
	zebraIdx := alphaIdx + len("### alpha\na\n\n")

	// alpha should come before zebra
	assert.Contains(t, rendered[alphaIdx:alphaIdx+10], "alpha")
	assert.Contains(t, rendered[zebraIdx:zebraIdx+10], "zebra")
}

func TestScratchpadThreadSafety(t *testing.T) {
	sp := NewScratchpad()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(4)
		go func(n int) {
			defer wg.Done()
			sp.Set("key", "value")
		}(i)
		go func(n int) {
			defer wg.Done()
			sp.Get("key")
		}(i)
		go func(n int) {
			defer wg.Done()
			sp.All()
		}(i)
		go func(n int) {
			defer wg.Done()
			sp.Render()
		}(i)
	}
	wg.Wait()
}

func TestScratchpadImplementsAccess(t *testing.T) {
	// Verify Scratchpad satisfies ScratchpadAccess interface.
	var _ ScratchpadAccess = NewScratchpad()
}
