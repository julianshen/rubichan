package analyzer

import (
	"testing"

	"github.com/julianshen/rubichan/internal/security"
)

func TestDataFlowAnalyzer(t *testing.T) {
	analyzerTestSuite(t, "dataflow", security.CategoryInjection, NewDataFlowAnalyzer)
}
