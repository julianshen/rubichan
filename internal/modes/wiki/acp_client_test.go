package wiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWikiACPClientProgress(t *testing.T) {
	client := &ACPClient{nextID: 1}

	assert.Equal(t, 0, client.Progress())

	client.SetProgress(50)
	assert.Equal(t, 50, client.Progress())

	client.SetProgress(100)
	assert.Equal(t, 100, client.Progress())
}

func TestWikiACPClientProgressClampingAbove(t *testing.T) {
	client := &ACPClient{nextID: 1}

	client.SetProgress(150)
	assert.Equal(t, 100, client.Progress())

	client.SetProgress(999)
	assert.Equal(t, 100, client.Progress())
}

func TestWikiACPClientProgressClampingBelow(t *testing.T) {
	client := &ACPClient{nextID: 1}

	client.SetProgress(-1)
	assert.Equal(t, 0, client.Progress())

	client.SetProgress(-100)
	assert.Equal(t, 0, client.Progress())
}

func TestWikiACPClientProgressResetToZero(t *testing.T) {
	client := &ACPClient{nextID: 1}

	client.SetProgress(75)
	assert.Equal(t, 75, client.Progress())

	client.SetProgress(0)
	assert.Equal(t, 0, client.Progress())
}

func TestWikiACPClientGetNextID(t *testing.T) {
	client := &ACPClient{nextID: 1}

	id1 := client.getNextID()
	id2 := client.getNextID()

	assert.Equal(t, int64(1), id1)
	assert.Equal(t, int64(2), id2)
}

func TestWikiACPClientCloseNilDispatcher(t *testing.T) {
	client := &ACPClient{nextID: 1}

	err := client.Close()
	assert.NoError(t, err)
}

func TestWikiGenerateOptionsFields(t *testing.T) {
	opts := GenerateOptions{
		Scope:      "internal/",
		Format:     "markdown",
		OutputDir:  "/tmp/wiki",
		MaxDepth:   3,
		IncludeAPI: true,
	}

	assert.Equal(t, "internal/", opts.Scope)
	assert.Equal(t, "markdown", opts.Format)
	assert.Equal(t, "/tmp/wiki", opts.OutputDir)
	assert.Equal(t, 3, opts.MaxDepth)
	assert.True(t, opts.IncludeAPI)
}
