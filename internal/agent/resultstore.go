package agent

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/store"
)

// ResultStore offloads large tool results to SQLite, keeping only compact
// references in conversation context.
type ResultStore struct {
	store     *store.Store
	sessionID string
	threshold int      // byte size above which results get offloaded
	refIDs    []string // track generated ref IDs for testing
}

// NewResultStore creates a ResultStore. Results exceeding threshold bytes
// are offloaded to the store.
func NewResultStore(s *store.Store, sessionID string, threshold int) *ResultStore {
	return &ResultStore{
		store:     s,
		sessionID: sessionID,
		threshold: threshold,
	}
}

// OffloadResult stores the content if it exceeds the threshold, returning a
// compact reference string. Returns the original content unchanged if below
// threshold.
func (rs *ResultStore) OffloadResult(toolName, toolUseID, content string) (string, error) {
	if len(content) <= rs.threshold {
		return content, nil
	}

	refID := uuid.New().String()
	if err := rs.store.SaveBlob(refID, rs.sessionID, toolName, content, len(content)); err != nil {
		// Graceful degradation: return original content if store fails.
		return content, nil
	}

	rs.refIDs = append(rs.refIDs, refID)

	preview := content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	return fmt.Sprintf(
		"[Tool result stored — %d bytes from %q.\nFirst %d chars: %s]\nUse the \"read_result\" tool with ref_id=%q to read specific portions.",
		len(content), toolName, min(len(content), 200), preview, refID,
	), nil
}

// Retrieve fetches the full stored result by reference ID.
func (rs *ResultStore) Retrieve(refID string) (string, error) {
	content, err := rs.store.GetBlob(refID)
	if err != nil {
		return "", fmt.Errorf("retrieve result %s: %w", refID, err)
	}
	if content == "" {
		return "", fmt.Errorf("result %s not found", refID)
	}
	return content, nil
}

// RefIDs returns the list of reference IDs generated so far (for testing).
func (rs *ResultStore) RefIDs() []string {
	return rs.refIDs
}
