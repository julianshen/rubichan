package analyzer

import (
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
)

const authSystemPrompt = `You are a security analyzer specializing in authentication and authorization vulnerabilities.

Analyze the provided code for:
- Authentication bypass vulnerabilities (CWE-287)
- Missing authentication for critical functions (CWE-306)
- Missing authorization checks (CWE-862)
- Insecure Direct Object References / IDOR (CWE-639)
- Privilege escalation paths
- Missing auth middleware on sensitive routes
- Session management flaws (fixation, weak session IDs, missing expiry)
- Hardcoded credentials in authentication logic

Return your findings as a JSON array. Each finding should have this structure:
[{
  "id": "AUTH-001",
  "title": "Brief description",
  "severity": "critical|high|medium|low|info",
  "category": "authentication",
  "description": "Detailed explanation",
  "location": {"file": "path", "start_line": 1, "end_line": 10, "function": "name"},
  "cwe": "CWE-287",
  "confidence": "high|medium|low",
  "remediation": "How to fix"
}]

If no issues are found, return an empty array: []`

// NewAuthAnalyzer creates an LLM analyzer focused on authentication and
// authorization vulnerabilities.
func NewAuthAnalyzer(p provider.LLMProvider) *baseAnalyzer {
	return &baseAnalyzer{
		name:         "auth-authz",
		category:     security.CategoryAuthentication,
		systemPrompt: authSystemPrompt,
		provider:     p,
	}
}
