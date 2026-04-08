package wiki_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/modes/wiki"
)

func TestWikiClientGenerateDocs(t *testing.T) {
	client := wiki.NewACPClient()

	resp, err := client.GenerateDocs("./", &wiki.GenerateOptions{
		Scope:           "project",
		OutputDir:       "docs/generated",
		IncludeSecurity: true,
	})
	if err != nil {
		t.Fatalf("generate docs failed: %v", err)
	}

	_ = resp
}

func TestWikiClientGenerateDocsWithDefaults(t *testing.T) {
	client := wiki.NewACPClient()

	resp, err := client.GenerateDocs("./", nil)
	if err != nil {
		t.Fatalf("generate docs with defaults failed: %v", err)
	}

	_ = resp
}

func TestWikiClientProgress(t *testing.T) {
	client := wiki.NewACPClient()

	if client.Progress() != 0 {
		t.Errorf("initial progress should be 0, got %d", client.Progress())
	}

	client.SetProgress(50)
	if client.Progress() != 50 {
		t.Errorf("got %d, want 50", client.Progress())
	}

	client.SetProgress(150) // Should clamp to 100
	if client.Progress() != 100 {
		t.Errorf("got %d, want 100 (clamped)", client.Progress())
	}

	client.SetProgress(-10) // Should clamp to 0
	if client.Progress() != 0 {
		t.Errorf("got %d, want 0 (clamped)", client.Progress())
	}
}
