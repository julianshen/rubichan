package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/checkpoint"
)

const maxUndoItems = 10

// UndoOverlay displays recent checkpoints for the user to select and restore.
type UndoOverlay struct {
	checkpoints []checkpoint.Checkpoint // newest first
	selected    int
	confirmed   bool
	cancelled   bool
	rewindAll   bool
	width       int
}

// NewUndoOverlay creates an undo overlay showing recent checkpoints.
func NewUndoOverlay(cps []checkpoint.Checkpoint, width int) *UndoOverlay {
	reversed := make([]checkpoint.Checkpoint, 0, len(cps))
	for i := len(cps) - 1; i >= 0; i-- {
		reversed = append(reversed, cps[i])
		if len(reversed) >= maxUndoItems {
			break
		}
	}
	return &UndoOverlay{
		checkpoints: reversed,
		width:       width,
	}
}

func (u *UndoOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return u, nil
	}
	if len(u.checkpoints) == 0 {
		u.cancelled = true
		return u, nil
	}
	switch keyMsg.Type {
	case tea.KeyUp:
		u.selected--
		if u.selected < 0 {
			u.selected = len(u.checkpoints) - 1
		}
	case tea.KeyDown:
		u.selected++
		if u.selected >= len(u.checkpoints) {
			u.selected = 0
		}
	case tea.KeyEnter:
		u.confirmed = true
	case tea.KeyEscape:
		u.cancelled = true
	case tea.KeyRunes:
		if len(keyMsg.Runes) > 0 && keyMsg.Runes[0] == 'a' {
			u.confirmed = true
			u.rewindAll = true
		}
	}
	return u, nil
}

func (u *UndoOverlay) View() string {
	if len(u.checkpoints) == 0 {
		return styleApprovalBorder.Width(u.boxWidth()).Render("No checkpoints available. Press any key to close.")
	}
	var b strings.Builder
	b.WriteString("Recent file changes:\n")
	for i, cp := range u.checkpoints {
		cursor := "  "
		if i == u.selected {
			cursor = "> "
		}
		name := filepath.Base(cp.FilePath)
		b.WriteString(fmt.Sprintf("%s%d. %s (turn %d, %s)\n", cursor, i+1, name, cp.Turn, cp.Operation))
	}
	b.WriteString("\n[↑↓] navigate  [Enter] undo  [a] undo all from turn  [Esc] cancel")
	return styleApprovalBorder.Width(u.boxWidth()).Render(b.String())
}

func (u *UndoOverlay) boxWidth() int {
	w := u.width - 4
	if w < 30 {
		w = 30
	}
	return w
}

func (u *UndoOverlay) Done() bool {
	return u.confirmed || u.cancelled
}

func (u *UndoOverlay) Result() any {
	if u.cancelled || len(u.checkpoints) == 0 {
		return nil
	}
	cp := u.checkpoints[u.selected]
	return UndoResult{
		Turn: cp.Turn,
		All:  u.rewindAll,
	}
}
