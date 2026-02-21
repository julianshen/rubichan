package analyzer

import (
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
)

const dataflowSystemPrompt = `You are a security analyzer specializing in data flow and taint analysis.

Analyze the provided code for:
- SQL injection via untrusted input reaching query builders (CWE-89)
- Command injection via untrusted input reaching os/exec or system calls (CWE-78)
- Path traversal via untrusted input reaching file system operations (CWE-22)
- Taint propagation from user input (HTTP params, form data, headers) to sensitive sinks
- Missing input sanitization or escaping before dangerous operations
- Second-order injection where stored data is later used unsafely

Return your findings as a JSON array. Each finding should have this structure:
[{
  "id": "FLOW-001",
  "title": "Brief description",
  "severity": "critical|high|medium|low|info",
  "category": "injection",
  "description": "Detailed explanation of the taint flow from source to sink",
  "location": {"file": "path", "start_line": 1, "end_line": 10, "function": "name"},
  "cwe": "CWE-89",
  "confidence": "high|medium|low",
  "remediation": "How to fix"
}]

If no issues are found, return an empty array: []`

// NewDataFlowAnalyzer creates an LLM analyzer focused on data flow and
// taint analysis vulnerabilities.
func NewDataFlowAnalyzer(p provider.LLMProvider) *baseAnalyzer {
	return &baseAnalyzer{
		name:         "dataflow",
		category:     security.CategoryInjection,
		systemPrompt: dataflowSystemPrompt,
		provider:     p,
	}
}
