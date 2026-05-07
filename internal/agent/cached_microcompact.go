package agent

import (
	"sync"
	"sync/atomic"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

const maxPendingEdits = 1000

// CachedMicrocompactService uses Anthropic's cache_edits API to remove tool
// results without invalidating the cached prefix.
type CachedMicrocompactService struct {
	mu           sync.Mutex
	pendingEdits []agentsdk.CacheEdit
	enabled      atomic.Bool
}

// NewCachedMicrocompactService creates a new service.
func NewCachedMicrocompactService(enabled bool) *CachedMicrocompactService {
	s := &CachedMicrocompactService{}
	s.enabled.Store(enabled)
	s.pendingEdits = make([]agentsdk.CacheEdit, 0, 16)
	return s
}

// IsEnabled returns whether the service is enabled.
func (s *CachedMicrocompactService) IsEnabled() bool {
	return s.enabled.Load()
}

// QueueEdit queues a deletion for a tool result by its tool_use_id.
func (s *CachedMicrocompactService) QueueEdit(toolUseID string) {
	if !s.enabled.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingEdits) >= maxPendingEdits {
		return // drop edits beyond cap to prevent unbounded growth
	}
	s.pendingEdits = append(s.pendingEdits, agentsdk.CacheEdit{
		Type:           "delete",
		CacheReference: toolUseID,
	})
}

// QueueEdits queues multiple deletions in a single lock acquisition.
func (s *CachedMicrocompactService) QueueEdits(toolUseIDs []string) {
	if !s.enabled.Load() || len(toolUseIDs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range toolUseIDs {
		if len(s.pendingEdits) >= maxPendingEdits {
			break
		}
		s.pendingEdits = append(s.pendingEdits, agentsdk.CacheEdit{
			Type:           "delete",
			CacheReference: id,
		})
	}
}

// TakeEdits returns a defensive copy of all pending edits and clears them.
func (s *CachedMicrocompactService) TakeEdits() []agentsdk.CacheEdit {
	s.mu.Lock()
	defer s.mu.Unlock()
	edits := make([]agentsdk.CacheEdit, len(s.pendingEdits))
	copy(edits, s.pendingEdits)
	s.pendingEdits = s.pendingEdits[:0]
	return edits
}

// HasEdits returns true if there are pending edits.
func (s *CachedMicrocompactService) HasEdits() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pendingEdits) > 0
}

// Reset clears all pending edits.
func (s *CachedMicrocompactService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingEdits = s.pendingEdits[:0]
}
