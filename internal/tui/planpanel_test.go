package tui

import (
	"testing"

	"github.com/julianshen/rubichan/internal/session"
	"github.com/stretchr/testify/assert"
)

func TestPlanItemIcon_Pending(t *testing.T) {
	assert.Equal(t, "○ ", planItemIcon(session.PlanStatusPending))
}

func TestPlanItemIcon_InProgress(t *testing.T) {
	assert.Equal(t, "⟳ ", planItemIcon(session.PlanStatusInProgress))
}

func TestPlanItemIcon_Completed(t *testing.T) {
	assert.Equal(t, "✓ ", planItemIcon(session.PlanStatusCompleted))
}

func TestPlanItemIcon_Failed(t *testing.T) {
	assert.Equal(t, "✗ ", planItemIcon(session.PlanStatusFailed))
}

func TestPlanItemIcon_ReverifyRequired(t *testing.T) {
	assert.Equal(t, "⟳ ", planItemIcon(session.PlanStatusReverifyRequired))
}

func TestRenderPlanPanel_Empty(t *testing.T) {
	result := renderPlanPanel(nil, 80)
	assert.Equal(t, "", result)

	result = renderPlanPanel([]session.PlanItem{}, 80)
	assert.Equal(t, "", result)
}

func TestRenderPlanPanel_SingleItem(t *testing.T) {
	items := []session.PlanItem{
		{Step: "Analyze code", Status: session.PlanStatusInProgress},
	}
	result := renderPlanPanel(items, 80)
	assert.Contains(t, result, "⟳ Analyze code")
	assert.NotContains(t, result, "more items")
}

func TestRenderPlanPanel_MultipleItems(t *testing.T) {
	items := []session.PlanItem{
		{Step: "Step 1", Status: session.PlanStatusCompleted},
		{Step: "Step 2", Status: session.PlanStatusInProgress},
		{Step: "Step 3", Status: session.PlanStatusPending},
	}
	result := renderPlanPanel(items, 80)
	assert.Contains(t, result, "✓ Step 1")
	assert.Contains(t, result, "⟳ Step 2")
	assert.Contains(t, result, "○ Step 3")
	assert.NotContains(t, result, "more items")
}

func TestRenderPlanPanel_Truncation(t *testing.T) {
	items := make([]session.PlanItem, maxPlanPanelLines+5)
	for i := range items {
		items[i] = session.PlanItem{
			Step:   "Step " + string(rune('A'+i%26)),
			Status: session.PlanStatusPending,
		}
	}
	result := renderPlanPanel(items, 80)
	assert.Contains(t, result, "5 more items")
	// Should not contain items beyond max
	assert.NotContains(t, result, items[maxPlanPanelLines+1].Step)
}

func TestPlanPanelHeight_Empty(t *testing.T) {
	height := planPanelHeight(nil)
	assert.Equal(t, 0, height)

	height = planPanelHeight([]session.PlanItem{})
	assert.Equal(t, 0, height)
}

func TestPlanPanelHeight_FewItems(t *testing.T) {
	items := []session.PlanItem{
		{Step: "Step 1", Status: session.PlanStatusPending},
		{Step: "Step 2", Status: session.PlanStatusInProgress},
	}
	height := planPanelHeight(items)
	// 2 lines of content + 2 for border (top/bottom)
	assert.Equal(t, 4, height)
}

func TestPlanPanelHeight_ManyItems(t *testing.T) {
	items := make([]session.PlanItem, maxPlanPanelLines+5)
	for i := range items {
		items[i] = session.PlanItem{Step: "Step", Status: session.PlanStatusPending}
	}
	height := planPanelHeight(items)
	// Clamped at maxPlanPanelLines + 2 for border
	assert.Equal(t, maxPlanPanelLines+2, height)
}
