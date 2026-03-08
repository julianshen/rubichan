package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffSummaryLabelTrimsIndentedMarkdownHeader(t *testing.T) {
	summary := "  ## Turn Summary: 2 file(s) changed\n\n- **foo.txt** modified"

	assert.Equal(t, "2 files changed", diffSummaryLabel(summary))
}
