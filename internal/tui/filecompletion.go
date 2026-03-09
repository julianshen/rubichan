package tui

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxFileCompletionCandidates = 8

// FileCompletionSource indexes project files for @ mention autocomplete.
type FileCompletionSource struct {
	mu      sync.RWMutex
	files   []string
	workDir string
	indexed bool
}

// NewFileCompletionSource creates a new source. The workDir is used for
// resolving relative paths.
func NewFileCompletionSource(workDir string) *FileCompletionSource {
	return &FileCompletionSource{workDir: workDir}
}

// SetFiles populates the file index. Files should be relative paths
// (e.g., from git ls-files). The list is sorted for stable ordering.
func (s *FileCompletionSource) SetFiles(files []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = make([]string, len(files))
	copy(s.files, files)
	sort.Strings(s.files)
	s.indexed = true
}

// Indexed returns true if the file list has been populated.
func (s *FileCompletionSource) Indexed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexed
}

// Files returns the full indexed file list.
func (s *FileCompletionSource) Files() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, len(s.files))
	copy(result, s.files)
	return result
}

// Match returns files matching the given query. It checks both prefix
// and substring (for filename-only matching).
func (s *FileCompletionSource) Match(query string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.indexed || query == "" {
		// Empty query returns all files (up to limit)
		if s.indexed && query == "" {
			return s.limitFiles(s.files)
		}
		return nil
	}
	query = strings.ToLower(query)
	var matches []string
	for _, f := range s.files {
		lower := strings.ToLower(f)
		if strings.HasPrefix(lower, query) || strings.Contains(lower, query) {
			matches = append(matches, f)
			if len(matches) >= maxFileCompletionCandidates {
				break
			}
		}
	}
	return matches
}

func (s *FileCompletionSource) limitFiles(files []string) []string {
	if len(files) <= maxFileCompletionCandidates {
		return files
	}
	return files[:maxFileCompletionCandidates]
}

// FileCompletionOverlay shows a dropdown of matching files when @ is typed.
type FileCompletionOverlay struct {
	source     *FileCompletionSource
	candidates []string
	selected   int
	visible    bool
	dismissed  bool
	width      int
	lastQuery  string
}

// NewFileCompletionOverlay creates a new file completion overlay.
func NewFileCompletionOverlay(source *FileCompletionSource, width int) *FileCompletionOverlay {
	return &FileCompletionOverlay{
		source: source,
		width:  width,
	}
}

// Update refreshes candidates based on input. Activates when input
// contains @ (looking at the last @ token in the input).
func (fo *FileCompletionOverlay) Update(input string) {
	// Find the last @ in the input
	atIdx := strings.LastIndex(input, "@")
	if atIdx < 0 {
		fo.visible = false
		fo.candidates = nil
		fo.dismissed = false
		fo.lastQuery = ""
		return
	}

	// Extract text after @
	rest := input[atIdx+1:]

	// If there's a space after the query, hide (file path accepted)
	if strings.Contains(rest, " ") {
		fo.visible = false
		fo.candidates = nil
		fo.lastQuery = ""
		return
	}

	if fo.dismissed {
		if rest == fo.lastQuery {
			return
		}
		fo.dismissed = false
	}

	// Query the source
	matches := fo.source.Match(rest)
	if rest != fo.lastQuery {
		fo.selected = 0
	}
	fo.lastQuery = rest
	fo.candidates = matches
	if fo.selected >= len(matches) && len(matches) > 0 {
		fo.selected = len(matches) - 1
	}
	fo.visible = len(matches) > 0
}

// HandleKey processes navigation keys when visible.
func (fo *FileCompletionOverlay) HandleKey(msg tea.KeyMsg) bool {
	if !fo.visible {
		return false
	}
	switch msg.Type {
	case tea.KeyUp:
		fo.selected--
		if fo.selected < 0 {
			fo.selected = len(fo.candidates) - 1
		}
		return true
	case tea.KeyDown:
		fo.selected++
		if fo.selected >= len(fo.candidates) {
			fo.selected = 0
		}
		return true
	case tea.KeyEscape:
		fo.visible = false
		fo.dismissed = true
		return true
	}
	return false
}

// HandleTab accepts the selected file path.
func (fo *FileCompletionOverlay) HandleTab() (accepted bool, value string) {
	if !fo.visible || len(fo.candidates) == 0 {
		return false, ""
	}
	return true, fo.candidates[fo.selected]
}

// Visible returns whether the overlay is showing.
func (fo *FileCompletionOverlay) Visible() bool {
	return fo.visible
}

// Candidates returns the current file matches.
func (fo *FileCompletionOverlay) Candidates() []string {
	return fo.candidates
}

// Selected returns the index of the selected candidate.
func (fo *FileCompletionOverlay) Selected() int {
	return fo.selected
}

// SetWidth updates the render width.
func (fo *FileCompletionOverlay) SetWidth(w int) {
	fo.width = w
}

// View renders the file completion overlay.
func (fo *FileCompletionOverlay) View() string {
	if !fo.visible || len(fo.candidates) == 0 {
		return ""
	}

	boxWidth := fo.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"}).
		Width(boxWidth)

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#5A56E0")).
		Foreground(lipgloss.Color("#FFFFFF"))

	start := 0
	total := len(fo.candidates)
	if total > maxFileCompletionCandidates {
		if fo.selected >= maxFileCompletionCandidates {
			start = fo.selected - maxFileCompletionCandidates + 1
		}
		if start+maxFileCompletionCandidates > total {
			start = total - maxFileCompletionCandidates
		}
	}
	end := start + maxFileCompletionCandidates
	if end > total {
		end = total
	}
	visible := fo.candidates[start:end]

	var rows []string
	for idx, f := range visible {
		i := start + idx
		row := fmt.Sprintf("  @%s", f)
		if i == fo.selected {
			row = selectedStyle.Render(row)
		}
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return borderStyle.Render(content)
}
