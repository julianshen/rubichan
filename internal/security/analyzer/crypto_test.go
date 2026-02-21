package analyzer

import (
	"testing"

	"github.com/julianshen/rubichan/internal/security"
)

func TestCryptoAnalyzer(t *testing.T) {
	analyzerTestSuite(t, "cryptography", security.CategoryCryptography, NewCryptoAnalyzer)
}
