package knowledgegraph

import (
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

func init() {
	// Register the internal implementation with the public package
	kg.RegisterOpenImpl(openGraph)
}
