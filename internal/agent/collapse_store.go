package agent

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/provider"
)

// CollapseCommit represents a committed archival of a message span.
type CollapseCommit struct {
	CollapseID        string
	SummaryUUID       string
	SummaryContent    string
	FirstArchivedUUID string
	LastArchivedUUID  string
	CommittedAt       time.Time
	TokensFreed       int
}

// CollapseStagedSpan represents a span of messages staged for archival.
type CollapseStagedSpan struct {
	StartUUID string
	EndUUID   string
	Summary   string
	Risk      float64
	StagedAt  time.Time
}

// CollapseStats reports the current state of the collapse store.
type CollapseStats struct {
	TotalCommits     int
	TotalTokensFreed int
	StagedCount      int
	IsEnabled        bool
}

// CollapseStore provides staged archival of conversation history.
type CollapseStore struct {
	mu      sync.RWMutex
	commits []CollapseCommit
	staged  []CollapseStagedSpan
	enabled bool
}

// NewCollapseStore creates a new CollapseStore.
func NewCollapseStore(enabled bool) *CollapseStore {
	return &CollapseStore{
		commits: make([]CollapseCommit, 0),
		staged:  make([]CollapseStagedSpan, 0),
		enabled: enabled,
	}
}

// IsEnabled returns whether the store is enabled.
func (s *CollapseStore) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// Stage adds a span to the staged list.
func (s *CollapseStore) Stage(span CollapseStagedSpan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staged = append(s.staged, span)
}

// Commit moves all staged spans to committed and returns the new commits.
func (s *CollapseStore) Commit() []CollapseCommit {
	s.mu.Lock()
	defer s.mu.Unlock()

	var committed []CollapseCommit
	for _, span := range s.staged {
		commit := CollapseCommit{
			CollapseID:        generateCollapseID(),
			SummaryUUID:       generateCollapseID(),
			SummaryContent:    span.Summary,
			FirstArchivedUUID: span.StartUUID,
			LastArchivedUUID:  span.EndUUID,
			CommittedAt:       time.Now(),
			TokensFreed:       0,
		}
		s.commits = append(s.commits, commit)
		committed = append(committed, commit)
	}
	s.staged = s.staged[:0]
	return committed
}

// DrainAll commits all staged spans without clearing staged (returns count).
func (s *CollapseStore) DrainAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := len(s.staged)
	for _, span := range s.staged {
		commit := CollapseCommit{
			CollapseID:        generateCollapseID(),
			SummaryUUID:       generateCollapseID(),
			SummaryContent:    span.Summary,
			FirstArchivedUUID: span.StartUUID,
			LastArchivedUUID:  span.EndUUID,
			CommittedAt:       time.Now(),
		}
		s.commits = append(s.commits, commit)
	}
	s.staged = s.staged[:0]
	return count
}

// HasCommits returns true if there are committed collapses.
func (s *CollapseStore) HasCommits() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.commits) > 0
}

// ProjectView replaces archived message ranges with summary messages.
func (s *CollapseStore) ProjectView(messages []provider.Message) []provider.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.commits) == 0 {
		return messages
	}

	var result []provider.Message
	inArchivedSpan := false
	var currentCommit *CollapseCommit

	for _, msg := range messages {
		if !inArchivedSpan {
			found := false
			for i := range s.commits {
				if msg.Metadata["id"] == s.commits[i].FirstArchivedUUID {
					inArchivedSpan = true
					commit := s.commits[i]
					currentCommit = &commit
					result = append(result, provider.Message{
						Role:     "assistant",
						Metadata: map[string]any{"id": commit.SummaryUUID},
						Content: []provider.ContentBlock{{
							Type: "text",
							Text: commit.SummaryContent,
						}},
					})
					found = true
					break
				}
			}
			if !found {
				result = append(result, msg)
			}
		} else {
			if currentCommit != nil && msg.Metadata["id"] == currentCommit.LastArchivedUUID {
				inArchivedSpan = false
				currentCommit = nil
			}
		}
	}

	return result
}

// GetStats returns statistics about the collapse store.
func (s *CollapseStore) GetStats() CollapseStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalTokens := 0
	for _, c := range s.commits {
		totalTokens += c.TokensFreed
	}

	return CollapseStats{
		TotalCommits:     len(s.commits),
		TotalTokensFreed: totalTokens,
		StagedCount:      len(s.staged),
		IsEnabled:        s.enabled,
	}
}

// Reset clears all commits and staged spans.
func (s *CollapseStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = s.commits[:0]
	s.staged = s.staged[:0]
}

// RestoreFromEntries restores commits from a slice.
func (s *CollapseStore) RestoreFromEntries(commits []CollapseCommit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = append(s.commits[:0], commits...)
}

func generateCollapseID() string {
	return uuid.NewString()
}
