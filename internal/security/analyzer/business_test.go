package analyzer

import (
	"testing"

	"github.com/julianshen/rubichan/internal/security"
)

func TestBusinessLogicAnalyzer(t *testing.T) {
	analyzerTestSuite(t, "business-logic", security.CategoryInputValidation, NewBusinessLogicAnalyzer)
}
