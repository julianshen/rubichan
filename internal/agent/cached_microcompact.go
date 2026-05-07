package agent

import (
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// CachedMicrocompactService uses Anthropic's cache_edits API to remove tool
// results without invalidating the cached prefix.
type CachedMicrocompactService struct {
	mu           sync.Mutex
	pendingEdits []agentsdk.CacheEdit
	enabled      bool
}

// NewCachedMicrocompactService creates a new service.
func NewCachedMicrocompactService(enabled bool) *CachedMicrocompactService {
	return &CachedMicrocompactService{
		enabled: enabled,
	}
}

// IsEnabled returns whether the service is enabled.
func (s *CachedMicrocompactService) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// QueueEdit queues a deletion for a tool result by its tool_use_id.
func (s *CachedMicrocompactService) QueueEdit(toolUseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return
	}
	s.pendingEdits = append(s.pendingEdits, agentsdk.CacheEdit{
		Type:           agentsdk.CacheEditDelete,
		CacheReference: toolUseID,
	})
}

// TakeEdits returns and clears all pending edits.
func (s *CachedMicrocompactService) TakeEdits() []agentsdk.CacheEdit {
	s.mu.Lock()
	defer s.mu.Unlock()
	edits := s.pendingEdits
	s.pendingEdits = nil
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
	s.pendingEdits = nil
}
