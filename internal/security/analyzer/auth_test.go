package analyzer

import (
	"testing"

	"github.com/julianshen/rubichan/internal/security"
)

func TestAuthAnalyzer(t *testing.T) {
	analyzerTestSuite(t, "auth-authz", security.CategoryAuthentication, NewAuthAnalyzer)
}
