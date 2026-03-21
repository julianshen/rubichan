package sandbox

import "strings"

// MatchDomain checks if domain matches any pattern in the allowlist.
// Patterns: exact ("github.com") or wildcard ("*.npmjs.org").
// Matching is case-insensitive. A wildcard pattern matches any single
// subdomain level but not the bare domain itself.
func MatchDomain(domain string, patterns []string) bool {
	domain = strings.ToLower(domain)
	for _, p := range patterns {
		p = strings.ToLower(p)
		if domain == p {
			return true
		}
		if strings.HasPrefix(p, "*.") {
			suffix := p[1:] // ".npmjs.org"
			if strings.HasSuffix(domain, suffix) && len(domain) > len(suffix) {
				return true
			}
		}
	}
	return false
}
