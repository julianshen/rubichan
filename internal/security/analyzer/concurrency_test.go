package analyzer

import (
	"testing"

	"github.com/julianshen/rubichan/internal/security"
)

func TestConcurrencyAnalyzer(t *testing.T) {
	analyzerTestSuite(t, "concurrency", security.CategoryRaceCondition, NewConcurrencyAnalyzer)
}
