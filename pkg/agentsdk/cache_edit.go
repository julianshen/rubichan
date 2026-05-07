package agentsdk

// CacheEdit represents an edit to remove content from the cached prefix.
// Currently only "delete" operations are supported.
type CacheEdit struct {
	Type           string `json:"type"`            // "delete"
	CacheReference string `json:"cache_reference"` // tool_use_id of block to remove
}
