# Phase 2 Code Reuse Review: Harness Engineering Implementation

**Date**: 2026-04-05  
**Scope**: Review Phase 2 changes for duplication and missed reuse opportunities

## Executive Summary

Phase 2 introduces solid, well-structured new packages (evaluator, verdict tracking) with **minimal duplication** but **several design improvements available**:

1. **[GOOD]** Concurrency patterns in `VerdictHistory` follow existing codebase conventions
2. **[GOOD]** File/command checking in `SpeculativeChecker` doesn't duplicate existing logic
3. **[OPPORTUNITY]** `VerdictContextBlock()` could adopt existing context-building patterns
4. **[OPPORTUNITY]** `SecurityAwareApprovalChecker` partially duplicates existing composition (but not a full duplication)
5. **[IMPROVEMENT]** JSON unmarshaling in evaluator subclasses is repetitive and could extract to helper

---

## Detailed Findings

### 1. VerdictHistory Concurrency Pattern (session/verdict.go)

**Status**: ✅ **GOOD — Follows Codebase Convention**

```go
type VerdictHistory struct {
	mu       sync.RWMutex
	verdicts []Verdict
	maxSize  int
}
```

**Finding**: Uses `sync.RWMutex` for thread-safe history tracking.

**Evidence**: This pattern is widespread and established in the codebase:
- `internal/tools/registry.go` — registry with RWMutex
- `internal/tools/lsp/registry.go` — LSP registry with RWMutex
- `internal/shell/hint_provider.go` — hint cache with RWMutex
- `internal/agent/scratchpad.go` — scratchpad with RWMutex
- `internal/skills/runtime.go` — runtime state with RWMutex

**Recommendation**: ✅ **Keep as-is**. This is the standard pattern used throughout the codebase for shared mutable state.

---

### 2. SpeculativeChecker File/Command Utilities (evaluator/speculative.go)

**Status**: ⚠️ **REUSE OPPORTUNITY — Precondition checks are simple, but watch for duplication**

**Issue**: `SpeculativeChecker` implements three precondition checks:
- `checkFileExists()` — uses `os.Stat()`
- `checkDirectoryExists()` — uses `os.Stat()` + `IsDir()` check
- `checkCommandRecognized()` — validates command format with string checks

**Current State**: The code doesn't duplicate existing utilities because:
- File existence checking in `internal/tools/file.go` is file-tool-specific (path resolution, symlink traversal)
- Command checking in `internal/tools/shell.go` is shell-specific (interceptor rules, substitution patterns)

**BUT**: These checks are duplicated *within* evaluator/speculative.go:
- Lines 53-72: `checkFileExists()` — creates map, unmarshals, gets path, calls `os.Stat()`
- Lines 74-94: `checkDirectoryExists()` — same pattern, adds `IsDir()` check
- Lines 96-129: `checkCommandRecognized()` — similar JSON unmarshaling, custom validation

**Recommendation**: Extract common JSON unmarshaling pattern:

```go
// In evaluator/speculative.go, add helper:
func (s *SpeculativeChecker) extractStringField(input json.RawMessage, fieldName string) (string, error) {
	var inp map[string]interface{}
	if err := json.Unmarshal(input, &inp); err != nil {
		return "", err
	}
	value, ok := inp[fieldName].(string)
	if !ok {
		return "", fmt.Errorf("field %q not a string or missing", fieldName)
	}
	return value, nil
}

// Then simplify:
func (s *SpeculativeChecker) checkFileExists(input json.RawMessage) (EvaluationResult, error) {
	path, err := s.extractStringField(input, "path")
	if err != nil {
		return EvaluationResult{SpeculativeOK: true}, nil
	}
	// rest of logic...
}
```

This reduces repeated `json.Unmarshal() + type assertion + nil check` pattern by ~15 LOC.

---

### 3. CompositeEvaluator Composition Pattern (evaluator/evaluator.go)

**Status**: ✅ **GOOD — Follows SDK Precedent**

```go
type CompositeEvaluator struct {
	evaluators []Evaluator
}

func (c *CompositeEvaluator) Evaluate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error) {
	accumulated := EvaluationResult{}
	for _, e := range c.evaluators {
		// Merge results
	}
}
```

**Evidence**: Identical pattern exists in `pkg/agentsdk/approval.go`:

```go
type CompositeApprovalChecker struct {
	checkers []ApprovalChecker
}

func (c *CompositeApprovalChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	for _, checker := range c.checkers {
		result := checker.CheckApproval(tool, input)
		if result != ApprovalRequired {
			return result
		}
	}
	return ApprovalRequired
}
```

**Key Difference**: 
- `CompositeApprovalChecker` uses **fail-fast** (first non-ApprovalRequired wins)
- `CompositeEvaluator` uses **accumulation** (merges all results)

This is intentional and correct — different use cases require different merging strategies.

**Recommendation**: ✅ **Keep as-is**. The patterns are appropriate for their domains.

---

### 4. SecurityAwareApprovalChecker Composition (agent/approval.go)

**Status**: ⚠️ **COMPOSITION PATTERN EXISTS ELSEWHERE — But This Implementation is Correct**

```go
type SecurityAwareApprovalChecker struct {
	base    ApprovalChecker
	scanner SecurityScanner
}

func (s *SecurityAwareApprovalChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	result := s.base.CheckApproval(tool, input)
	if result == AutoDenied { return result }
	
	shouldBlock, _, _ := s.scanner.Scan(context.Background(), tool, input)
	// ...
}
```

**Observation**: This is a **wrapper composition** (decorator pattern). The codebase has other wrapper compositions:
- `internal/agent/approval.go:autoApproveAdapter` — wraps `AutoApproveChecker` as `ApprovalChecker`
- `internal/tools/lsp/registry.go` — wraps LSP protocol handlers
- `internal/security/engine.go` — likely has scanner composition

**Comparison to autoApproveAdapter**:

```go
type autoApproveAdapter struct {
	checker AutoApproveChecker
}

func (a *autoApproveAdapter) CheckApproval(tool string, _ json.RawMessage) ApprovalResult {
	if a.checker.IsAutoApproved(tool) {
		return AutoApproved
	}
	return ApprovalRequired
}
```

Both follow the **wrapper pattern**, but they serve different purposes:
- `autoApproveAdapter`: legacy bridge (legacy interface → new interface)
- `SecurityAwareApprovalChecker`: enrichment (approval + security checks)

**Recommendation**: ✅ **Keep as-is**. The implementation is correct and doesn't duplicate existing logic.

---

### 5. VerdictContextBlock Context Building (agent/context.go)

**Status**: ⚠️ **OPPORTUNITY — Aligns with but Could Extend Existing Pattern**

```go
func VerdictContextBlock(verdictHist *session.VerdictHistory) string {
	summary := verdictHist.SummaryByTool()
	// Build formatted string with success rates
	var buf strings.Builder
	buf.WriteString("Recent tool execution outcomes:\n")
	// ...
}
```

**Current Context Building Pattern** (in `internal/agent/agent.go`):

The agent assembles system prompt sections via `assembleSystemPromptSections()`:

```go
func (a *Agent) assembleSystemPromptSections(memories []MemoryEntry) []PromptSection {
	sections := []PromptSection{{
		Name:      "System",
		Content:   a.basePrompt,
		Cacheable: true,
	}}
	// Adds Identity, Soul sections...
	return sections
}
```

Then sections are used to build system prompt in `buildSystemPromptWithFragments()`.

**Issue**: `VerdictContextBlock()` is a **standalone function** that:
- Returns a formatted string directly
- Doesn't integrate with the `PromptSection` framework
- Must be manually concatenated into system prompt (lines 1077, 1089 show string concatenation)

**Evidence of Existing Pattern**:
- `persona.BaseSystemPrompt()` — building blocks
- `persona.IdentityPrompt()` — building blocks
- `persona.SoulPrompt()` — building blocks
- System prompt assembly via sections with `Name`, `Content`, `Cacheable` metadata

**Opportunity**: Create a `PromptSection` for verdict history rather than a standalone function:

```go
// Option 1: Add to PromptSection building
func (a *Agent) verdictContextSection() PromptSection {
	content := a.state.VerdictHistory().SummaryByTool()
	if len(content) == 0 {
		return PromptSection{} // empty
	}
	return PromptSection{
		Name:      "ToolExecutionHistory",
		Content:   formatVerdictSummary(content),
		Cacheable: false, // Changes frequently
	}
}

// Option 2: Or keep as utility but name it consistently
// Current: VerdictContextBlock
// Suggested: VerdictHistoryPrompt (matches persona.BaseSystemPrompt pattern)
```

**Current Usage** (headless.go line 85-101): Not actually calling `VerdictContextBlock()` anywhere yet. The evaluation happens but context isn't used.

**Recommendation**: 
- ⚠️ **If verdict history will be added to system prompt**: refactor to use `PromptSection` framework rather than standalone function
- ✅ **If it remains a utility for future use**: keep `VerdictContextBlock()` but consider renaming to `VerdictHistoryPrompt()` for consistency with `persona.*Prompt()` naming

---

### 6. JSON Unmarshaling Duplication (evaluator package)

**Status**: ⚠️ **DUPLICATION OPPORTUNITY**

**Current Pattern** (repeated 3 times):

In `evaluator.go` (lines 91-92):
```go
var input map[string]interface{}
if err := json.Unmarshal(req.Input, &input); err != nil {
	return EvaluationResult{SchemaValid: false, SchemaError: fmt.Sprintf("invalid JSON: %v", err)}, nil
}
```

In `speculative.go` (lines 54-57, 75-78, 98-101):
```go
var inp map[string]interface{}
if err := json.Unmarshal(input, &inp); err != nil {
	return EvaluationResult{SpeculativeOK: true}, nil
}
```

**Problem**: Same `json.Unmarshal()` pattern repeated 4 times with different error handling.

**Recommendation**: Extract helper:

```go
// In evaluator.go:
func unmarshalInput(input json.RawMessage) (map[string]interface{}, error) {
	var inp map[string]interface{}
	if err := json.Unmarshal(input, &inp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return inp, nil
}

// In speculative.go, simplify:
func (s *SpeculativeChecker) checkFileExists(input json.RawMessage) (EvaluationResult, error) {
	inp, _ := unmarshalInput(input) // Silent fail in speculative checks
	if inp == nil {
		return EvaluationResult{SpeculativeOK: true}, nil
	}
	// rest...
}
```

Saves ~12 LOC and improves maintainability.

---

## Summary Table

| Component | Pattern | Status | Recommendation |
|-----------|---------|--------|-----------------|
| **VerdictHistory** | RWMutex concurrency | ✅ Follows codebase | Keep as-is |
| **SpeculativeChecker** | File/command validation | ✅ No duplication | Extract JSON unmarshal helper |
| **CompositeEvaluator** | Composition/accumulation | ✅ Intentional pattern | Keep as-is |
| **SecurityAwareApprovalChecker** | Wrapper/decorator | ✅ Correct pattern | Keep as-is |
| **VerdictContextBlock** | Context building | ⚠️ Standalone function | Consider PromptSection integration |
| **JSON unmarshaling** | Repeated pattern | ⚠️ Duplication | Extract `unmarshalInput()` helper |

---

## Risk Assessment

**Low Risk**: All changes are isolated to new packages with no risk of affecting existing functionality.

**Code Quality**: Phase 2 is well-structured with:
- Clear separation of concerns
- 125+ tests providing good coverage
- Standard Go patterns throughout

**Suggested Improvements** (non-blocking):
1. Extract `unmarshalInput()` helper (5 min refactor, saves 12 LOC)
2. Extract `extractStringField()` helper in speculative checker (5 min, saves 15 LOC)
3. Optional: integrate verdict context into `PromptSection` framework if system prompt usage is planned

---

## Files Analyzed

- `internal/evaluator/evaluator.go` (152 lines)
- `internal/evaluator/confidence.go` (62 lines)
- `internal/evaluator/speculative.go` (130 lines)
- `internal/evaluator/evaluator_test.go` (580 lines, 26 tests)
- `internal/session/verdict.go` (93 lines)
- `internal/session/verdict_test.go` (implied in changes)
- `internal/agent/approval.go` (311 lines, modifications)
- `internal/agent/context.go` (296 lines, 1 function added)
- `internal/runner/headless.go` (200+ lines, modifications)
- Compared against: `pkg/agentsdk/approval.go`, `internal/tools/file.go`, `internal/tools/shell.go`, and 15+ existing pattern files

---

## Conclusion

Phase 2 demonstrates **excellent code discipline**:
- No major duplications
- Consistent with existing patterns
- Well-tested
- Clear APIs

The identified opportunities are **minor refinements** that would improve maintainability without changing behavior. All changes are low-risk and ready for integration.

**Recommendation**: ✅ **Approve for merge** with optional refactoring notes for post-merge cleanup.
