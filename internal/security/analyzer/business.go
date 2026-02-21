package analyzer

import (
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
)

const businessSystemPrompt = `You are a security analyzer specializing in business logic vulnerabilities.

Analyze the provided code for:
- Logic flaws that bypass intended business rules (CWE-840)
- Negative quantity or negative price exploits in e-commerce/financial code
- Race-to-credit conditions where timing allows double spending or rewards
- Bypass conditions that skip validation through unexpected input combinations
- Missing boundary checks on business-critical values
- State machine violations where operations happen in wrong order
- Price manipulation through parameter tampering
- Referral or coupon abuse through logic gaps

Return your findings as a JSON array. Each finding should have this structure:
[{
  "id": "BIZ-001",
  "title": "Brief description",
  "severity": "critical|high|medium|low|info",
  "category": "input-validation",
  "description": "Detailed explanation of the business logic flaw",
  "location": {"file": "path", "start_line": 1, "end_line": 10, "function": "name"},
  "cwe": "CWE-840",
  "confidence": "high|medium|low",
  "remediation": "How to fix"
}]

If no issues are found, return an empty array: []`

// NewBusinessLogicAnalyzer creates an LLM analyzer focused on business logic
// vulnerabilities.
func NewBusinessLogicAnalyzer(p provider.LLMProvider) *baseAnalyzer {
	return &baseAnalyzer{
		name:         "business-logic",
		category:     security.CategoryInputValidation,
		systemPrompt: businessSystemPrompt,
		provider:     p,
	}
}
