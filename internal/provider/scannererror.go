package provider

import "errors"

// WrapScannerError converts an SSE scanner error into a typed *ProviderError.
// If err is already a *ProviderError, its Provider and RequestID fields are
// backfilled when empty so upstream watchdog errors keep their original kind
// and metadata. Otherwise a fresh ErrStreamError is constructed.
func WrapScannerError(err error, providerName, requestID string) *ProviderError {
	var pe *ProviderError
	if errors.As(err, &pe) {
		if pe.Provider == "" {
			pe.Provider = providerName
		}
		if pe.RequestID == "" {
			pe.RequestID = requestID
		}
		return pe
	}
	return &ProviderError{
		Kind:      ErrStreamError,
		Provider:  providerName,
		Message:   err.Error(),
		RequestID: requestID,
	}
}
