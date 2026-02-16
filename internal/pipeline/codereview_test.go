package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildReviewPrompt(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n package main\n+func hello() {}\n"
	prompt := BuildReviewPrompt(diff)

	require.NotEmpty(t, prompt)
	assert.Contains(t, prompt, diff)
	assert.Contains(t, prompt, "review")
}

func TestBuildReviewPromptEmpty(t *testing.T) {
	prompt := BuildReviewPrompt("")
	assert.Contains(t, prompt, "no changes")
}
