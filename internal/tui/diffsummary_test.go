package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffSummaryLabelTrimsIndentedMarkdownHeader(t *testing.T) {
	summary := "  ## Some Title\n\nDetails"

	assert.Equal(t, "Some Title", diffSummaryLabel(summary))
}
