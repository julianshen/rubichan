package cmux

// Caller is the interface for making cmux JSON-RPC calls.
// Both Client and cmuxtest.MockClient implement this.
type Caller interface {
	Call(method string, params any) (*Response, error)
	Identity() *Identity
}
