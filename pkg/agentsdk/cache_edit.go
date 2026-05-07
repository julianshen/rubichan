package agentsdk

// CacheEditType represents the type of cache edit operation.
type CacheEditType string

const (
	CacheEditDelete CacheEditType = "delete"
)

// CacheEdit represents an edit to remove content from the cached prefix.
type CacheEdit struct {
	Type           CacheEditType `json:"type"`
	CacheReference string        `json:"cache_reference"`
}

// CacheReference marks a block as referenceable for cache edits.
type CacheReference struct {
	Type string `json:"type"` // "cache_reference"
	ID   string `json:"id"`   // references tool_use_id
}
