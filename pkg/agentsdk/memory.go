package agentsdk

// MemoryStore is the persistence interface for cross-session memories.
type MemoryStore interface {
	SaveMemory(workingDir, tag, content string) error
	LoadMemories(workingDir string) ([]MemoryEntry, error)
}

// MemoryEntry represents a single cross-session memory.
type MemoryEntry struct {
	Tag     string
	Content string
}
