package runner

import (
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/security"
)

// ExitCodeFromFindings returns 1 if any finding has severity at or above
// the failOn threshold, 0 otherwise. An empty failOn disables gating.
func ExitCodeFromFindings(findings []output.SecurityFinding, failOn string) int {
	if failOn == "" {
		return 0
	}
	threshold := security.SeverityRank(security.Severity(failOn))
	if threshold == 0 {
		return 0
	}
	for _, f := range findings {
		if security.SeverityRank(security.Severity(f.Severity)) >= threshold {
			return 1
		}
	}
	return 0
}
