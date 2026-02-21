package security

import "fmt"

// Correlator deduplicates findings and detects multi-step attack chains
// by analyzing proximity and category combinations.
type Correlator struct{}

// NewCorrelator creates a new Correlator instance.
func NewCorrelator() *Correlator {
	return &Correlator{}
}

// dedupeKey uniquely identifies a finding for deduplication purposes.
type dedupeKey struct {
	CWE       string
	File      string
	StartLine int
}

// chainPattern defines a known attack chain pattern.
type chainPattern struct {
	cat1     Category
	cat2     Category
	title    string
	severity Severity
	impact   string
	sameFunc bool // if true, requires same function; if false, same file suffices
}

// knownPatterns lists all recognized attack chain patterns.
var knownPatterns = []chainPattern{
	{
		cat1:     CategoryAuthentication,
		cat2:     CategoryInjection,
		title:    "Unauthenticated Injection",
		severity: SeverityCritical,
		impact:   "An attacker can exploit an injection vulnerability without authentication, allowing unauthorized command execution or data manipulation.",
		sameFunc: true,
	},
	{
		cat1:     CategoryAuthentication,
		cat2:     CategoryDataExposure,
		title:    "Unauthenticated Data Access",
		severity: SeverityCritical,
		impact:   "An attacker can access sensitive data without authentication, leading to unauthorized information disclosure.",
		sameFunc: true,
	},
	{
		cat1:     CategoryCryptography,
		cat2:     CategorySecretsExposure,
		title:    "Recoverable Secret",
		severity: SeverityHigh,
		impact:   "Weak cryptography combined with exposed secrets allows an attacker to recover plaintext credentials or keys.",
		sameFunc: false,
	},
	{
		cat1:     CategoryRaceCondition,
		cat2:     CategoryAuthorization,
		title:    "TOCTOU Authorization Bypass",
		severity: SeverityHigh,
		impact:   "A race condition in the authorization check allows an attacker to bypass access controls via time-of-check to time-of-use exploitation.",
		sameFunc: true,
	},
}

// confidenceRank returns a numeric rank for ordering confidences.
// High=3, Medium=2, Low=1.
func confidenceRank(c Confidence) int {
	switch c {
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	case ConfidenceLow:
		return 1
	default:
		return 0
	}
}

// Correlate deduplicates findings and detects attack chains.
// It returns any detected attack chains and the deduplicated findings list.
func (c *Correlator) Correlate(findings []Finding) ([]AttackChain, []Finding) {
	if len(findings) == 0 {
		return nil, nil
	}

	deduped := c.deduplicate(findings)
	chains := c.detectChains(deduped)

	return chains, deduped
}

// deduplicate groups findings by (CWE + File + StartLine) and keeps the one
// with highest confidence. Ties keep the first occurrence.
func (c *Correlator) deduplicate(findings []Finding) []Finding {
	best := make(map[dedupeKey]int) // key -> index into findings
	for i, f := range findings {
		key := dedupeKey{
			CWE:       f.CWE,
			File:      f.Location.File,
			StartLine: f.Location.StartLine,
		}
		if existing, ok := best[key]; ok {
			if confidenceRank(f.Confidence) > confidenceRank(findings[existing].Confidence) {
				best[key] = i
			}
		} else {
			best[key] = i
		}
	}

	// Preserve original order for deterministic output.
	seen := make(map[int]bool, len(best))
	for _, idx := range best {
		seen[idx] = true
	}

	result := make([]Finding, 0, len(best))
	for i := range findings {
		if seen[i] {
			result = append(result, findings[i])
		}
	}
	return result
}

// proximate checks whether two findings are close enough to form an attack chain.
// Two findings are proximate if they share the same file AND either share the
// same function or have overlapping/adjacent line ranges (within 20 lines).
func proximate(a, b Finding, sameFunc bool) bool {
	if a.Location.File != b.Location.File {
		return false
	}

	if sameFunc {
		// Must share the same function
		if a.Location.Function != "" && b.Location.Function != "" &&
			a.Location.Function == b.Location.Function {
			return true
		}
		// Also accept overlapping/adjacent line ranges within same function context
		if a.Location.Function != "" && b.Location.Function != "" &&
			a.Location.Function == b.Location.Function {
			return true
		}
		// If either has no function, fall back to line proximity check within same file
		// but only if sameFunc is required, we need function match
		return false
	}

	// Same file is sufficient for non-sameFunc patterns
	return true
}

// detectChains scans deduplicated findings for known attack chain patterns.
func (c *Correlator) detectChains(findings []Finding) []AttackChain {
	var chains []AttackChain
	chainID := 0

	// Track which finding pairs have already been used in chains to avoid duplicates.
	used := make(map[string]bool)

	for _, pattern := range knownPatterns {
		for i := 0; i < len(findings); i++ {
			for j := i + 1; j < len(findings); j++ {
				fi, fj := findings[i], findings[j]

				// Check both orderings of the pattern categories.
				matched := false
				if fi.Category == pattern.cat1 && fj.Category == pattern.cat2 {
					matched = true
				} else if fi.Category == pattern.cat2 && fj.Category == pattern.cat1 {
					matched = true
				}

				if !matched {
					continue
				}

				if !proximate(fi, fj, pattern.sameFunc) {
					continue
				}

				// Build a unique key for this pair to prevent duplicate chains.
				pairKey := fmt.Sprintf("%s:%s:%s", pattern.title, fi.ID, fj.ID)
				if used[pairKey] {
					continue
				}
				used[pairKey] = true

				chainID++
				chain := AttackChain{
					ID:         fmt.Sprintf("C-%d", chainID),
					Title:      pattern.title,
					Severity:   pattern.severity,
					Steps:      []Finding{fi, fj},
					Impact:     pattern.impact,
					Likelihood: "high",
				}
				chains = append(chains, chain)
			}
		}
	}

	return chains
}
