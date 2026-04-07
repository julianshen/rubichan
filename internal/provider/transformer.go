package provider

// MessageTransformer converts a CompletionRequest into the provider-specific
// JSON request body. Each provider adapter implements this interface to
// separate serialization from HTTP transport.
type MessageTransformer interface {
	// ToProviderJSON converts a CompletionRequest into the provider-specific
	// JSON request body ready for HTTP POST.
	ToProviderJSON(req CompletionRequest) ([]byte, error)
}
