package tui

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	_ "github.com/julianshen/rubichan/internal/provider/ollama"
	"github.com/julianshen/rubichan/internal/testutil"
)

func TestModelsFetchedMsgSuccessOpensPicker(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	// Two models are used deliberately: NewModelPicker auto-selects (and
	// skips form construction) when given exactly one choice, which would
	// make Init() return nil and defeat this test's purpose of verifying
	// the huh form actually initializes.
	updated, cmd := m.Update(ModelsFetchedMsg{
		Models: []provider.Model{
			{ID: "llama3.2:latest", Name: "llama3.2:latest"},
			{ID: "mistral:latest", Name: "mistral:latest"},
		},
	})
	m = updated.(*Model)

	assert.Equal(t, StateModelPickerOverlay, m.state)
	require.NotNil(t, m.activeOverlay)
	assert.NotNil(t, cmd) // huh form Init returns a command
}

func TestModelsFetchedMsgEmptyListShowsMessage(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	before := m.content.String()
	updated, cmd := m.Update(ModelsFetchedMsg{Models: nil})
	m = updated.(*Model)

	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.activeOverlay)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String()[len(before):], "No models available")
}

func TestModelsFetchedMsgErrorShowsMessage(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	before := m.content.String()
	updated, cmd := m.Update(ModelsFetchedMsg{Err: assert.AnError})
	m = updated.(*Model)

	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.activeOverlay)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String()[len(before):], "Failed to list Ollama models")
}

func TestFetchOllamaModelsQueriesTheConfiguredServer(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [{"name": "llama3.2:latest", "size": 1}]}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = srv.URL

	m := NewModel(nil, "test", "model", 10, "", cfg, nil)
	cmd := m.fetchOllamaModels()
	require.NotNil(t, cmd)

	msg := cmd()
	fetched, ok := msg.(ModelsFetchedMsg)
	require.True(t, ok)
	require.NoError(t, fetched.Err)
	require.Len(t, fetched.Models, 1)
	assert.Equal(t, "llama3.2:latest", fetched.Models[0].ID)
}

func TestSpinnerTicksDuringFetchingModels(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	updated, cmd := m.Update(m.spinner.Tick())
	m = updated.(*Model)
	assert.NotNil(t, cmd, "spinner must keep ticking while fetching models, or the animation freezes")
	_ = m
}
