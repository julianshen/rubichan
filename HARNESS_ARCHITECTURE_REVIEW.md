# Rubichan Harness Engineering Architecture Review

**Date:** 2026-04-04  
**Reviewer:** Claude Code  
**Framework Reference:** Anthropic's Harness Design + Martin Fowler's Harness Engineering  
**Scope:** Comprehensive mapping of control types, multi-agent patterns, and gap analysis

---

## Executive Summary

Rubichan demonstrates **strong foundational harness alignment** with mature computational controls and emerging multi-agent patterns. The architecture excels in:

1. **Computational controls** (tests, linters, type system) — strict TDD enforced, >90% coverage
2. **Tool-layer feedforward controls** — approval system with rule-based trust evaluation
3. **Structured artifact handoffs** — session persistence, checkpoint snapshots, verification snapshots
4. **Two-phase security hybrid** — static scanners + LLM analyzers with prioritization

**Key gaps and weaknesses:**

1. **Evaluator pattern underused** — no systematic skeptical assessment phase between planning and execution
2. **Context management opaque to LLM** — token budgets and compaction signals not visible; summarization lacks explicit quality tuning
3. **Inferential controls scattered** — security analyzers exist but aren't integrated into agent turn loop; missing feedback loop from verification to planner
4. **Multi-agent separation weak** — no explicit planner/generator/evaluator separation; subagent system is tool-centric, not behavior-centric
5. **Feedback loops incomplete** — verification snapshots collected but not fed back to adjust planning; diff tracking doesn't inform future turn strategy

---

## 1. Harness Control Classification

### 1.1 Computational Controls (MATURE)

Rubichan's computational harness is **mature** — fast, deterministic, early in the lifecycle.

#### Type System (Go)
- **Strength:** Interface-based boundaries throughout (Provider, Tool, LLMAnalyzer, Middleware, Skill)
- **Where used:** Core agent loop, security engine, tool registry
- **Gap:** Tool input validation only happens at execution time; no schema validation before approval decision

#### Test Coverage (>90%)
- **Strength:** Strict TDD enforced in CLAUDE.md; all packages >90% coverage
- **Implementation:** `go test -cover ./...` is blocking gate; coverage verified in PRs
- **Gap:** Integration tests are sparse; most tests are unit-level. Harness-level tests (approval → tool execution → context management) rare.

#### Linters & Formatters
- **Strength:** golangci-lint + gofmt block commits; zero-linter-warnings policy enforced
- **Implementation:** CI/CD gates; local pre-commit validation
- **Gap:** Linters validate syntax, not architecture. No AST-level harness consistency checks.

#### Structured Concurrency (`sourcegraph/conc`)
- **Strength:** Goroutine pools with error handling; no unbounded parallelism
- **Used in:** Security engine parallel scanners, provider retry logic
- **Gap:** No rate limiting on agent turn parallelism; tool execution can spawn many processes simultaneously

---

### 1.2 Feedforward Controls (STRONG → MODERATE)

#### 1.2.1 **Approval System (Tool-Layer Guard)**

**Component:** `internal/agent/approval.go` + `TrustRuleChecker`

**Mechanism:**
- Rule-based approval (regex + glob patterns) checked before tool execution
- Deny rules block immediately; allow rules auto-approve without user intervention
- Input-sensitive: patterns match against JSON string values, not just tool name
- Composite support: legacy AutoApproveChecker can wrap tool-name-only rules

**Classification:** Feedforward control — guides what tools can run without human approval.

**Strength:**
- Glob syntax is user-friendly: `"shell(git *)"` more intuitive than regex
- Deny-first semantics prevent false positives
- Pre-evaluated at tool dispatch time, before handler execution

**Weaknesses:**
- **No input validation schema:** Patterns only match strings; numeric inputs (timeouts, retry counts) bypass checking
- **Single-shot evaluation:** Rule result is cached; later turns can't refine based on execution outcome
- **Approval function still opaque:** If rule says "require approval," the user callback `ApprovalFunc` is invoked with no context about *why* approval is needed

**Gap:** Approval system is feedforward but disconnected from feedback. If a "git commit" tool is auto-approved but commits fail, there's no loop back to tighten the rules.

#### 1.2.2 **System Prompt & Instructions (Semantic Guidance)**

**Components:** 
- Base system prompt (CLAUDE.md specifies structure)
- Injected sections: IDENTITY.md, SOUL.md, AGENT.md, skill-contributed prompts
- Static prompt sections assembled at construction time

**Mechanism:**
- All guide the model toward correct outputs without hard constraints
- Skill system allows domain-specific prompts (e.g., "when working with iOS builds...")
- Pipeline middleware can inject tool descriptions dynamically

**Strength:**
- Multi-source prompt assembly is explicit and testable
- Skill hooks allow context-sensitive guidance
- Modular structure (AGENT.md for project rules, SOUL.md for persona)

**Weakness:**
- Prompts are **static at agent construction time** — can't adapt based on turn outcomes
- No mechanism to adjust system prompt based on verification failures or tool errors
- Evaluator tuning (skeptical assessment) is not represented in prompts

---

### 1.3 Inferential Controls (MODERATE → WEAK)

#### 1.3.1 **Security Engine (Two-Phase Hybrid)**

**Component:** `internal/security/engine.go`

**Mechanism:**
1. **Phase 1:** Static scanners run concurrently (secrets, SAST, license, Apple-specific)
2. **Prioritization:** High-risk segments selected for expensive LLM analysis
3. **Phase 2:** LLM analyzers examine prioritized chunks
4. **Correlation:** Deduplicate findings and detect attack chains

**Classification:** Inferential control — AI-powered semantic analysis on code segments.

**Strength:**
- Two-phase separation reduces cost: static findings guide what the LLM examines
- Parallel execution: scanners and analyzers run via `sourcegraph/conc` pools
- Output formats (JSON, SARIF, GitHub PR comments, CycloneDX) for different consumers

**Weakness:**
- **Disconnected from agent loop:** Security engine is invoked only in headless code-review mode, not during interactive agent turns
- **No feedback to LLM:** When security findings are discovered, they don't influence the agent's next move or prompt adjustment
- **Manual invocation required:** Agent doesn't automatically re-run security analysis after tool execution; no integration point in the turn loop

**Gap:** Security is a bolt-on safety check, not woven into the turn decision loop. Verification snapshots exist but don't feed back to planning.

#### 1.3.2 **Verification Snapshot & Evidence Collection**

**Component:** `internal/runner/headless.go` (buildEvidenceSummary, backendVerificationVerdict, etc.)

**Mechanism:**
- After each turn, evidence is collected from tool calls (file edits, builds, tests, runtime checks)
- Verdict rules applied: frontend requires build post-edit; backend requires schema + runtime + API
- Snapshot returned in run result

**Strength:**
- Semantic understanding of task completion (tool calls tell a story)
- Task-aware verdicts (frontend vs. backend logic differs)
- Snapshot format allows external inspection and replay

**Weakness:**
- **Verdict is a post-hoc label, not steering signal:** Agent doesn't see the verdict; it only appears in output
- **No loop back to planning:** If verdict is "failed", agent doesn't know; next turn starts fresh
- **Manual interpretation required:** Evidence summary is prose, not actionable structured feedback

---

## 2. Multi-Agent Architecture Assessment

### 2.1 Current Multi-Agent Patterns

Rubichan has **rudimentary multi-agent support**:

#### Subagent System
- **Location:** `internal/agent/subagent.go`
- **Mechanism:** Child agent spawned by parent via `subagent()` tool
- **Lifetime:** Child runs independently; parent resumed when child completes
- **Separation:** Child inherits parent's session/context/skill runtime but can have independent tool registry

**Current role:** Tool-invocation delegation only (e.g., "run lint checks on project X"). Not behavior-centric.

#### Wake Manager (Background Tasks)
- **Location:** `internal/agent/wake.go`
- **Mechanism:** Tracks background subagent completions; parent can receive "wake events" on completion
- **Coupling:** Implicit — parent polls for completion via `WakeManager`

**Current role:** Async tool delegation, not semantic task decomposition.

### 2.2 Missing Multi-Agent Patterns

#### 1. **No Explicit Planner**
- Agent loop is monolithic: receive prompt → build request → call LLM → execute tools → loop
- No separate "plan decomposition" phase that breaks complex tasks into steps
- Skill system has workflow type, but workflows are sequential tool chains, not semantic task graphs

#### 2. **No Generator/Evaluator Separation**
- LLM generates code/plans; same LLM evaluates its own output in next turn
- No second evaluator to skeptically assess before execution
- Anthropic pattern: generator produces candidate; evaluator rates it; planner adjusts; loop repeats

#### 3. **Context Opaque to Subagents**
- When subagent inherits tool registry, it doesn't know *why* parent delegated to it
- No "delegation contract" specifying: success criteria, resource bounds, fallback strategy
- Subagent loop is identical to parent loop; no behavioral difference

---

## 3. Context Management & Artifact Handoffs

### 3.1 Session Persistence (Strong Feedforward)

**Component:** `internal/store/store.go` (SQLite backing)

**Mechanism:**
- Session created with unique ID; messages persisted after each turn
- Resume-on-demand: load session, restore conversation state, continue
- Snapshot support: compressed conversation state for rapid resume

**Strength:**
- Deterministic storage layer; messages are canonical facts
- Supports interactive mode use case (user can close terminal, resume later)
- Compaction strategy choices explicit: tool clearing → summarization → truncation

**Gap:** Session resume doesn't carry over "planning state." If session was interrupted mid-plan, resumed agent has no memory of the plan (only conversation history).

### 3.2 Checkpoint Management (Artifact Snapshots)

**Component:** `internal/checkpoint/checkpoint.go`

**Mechanism:**
- Before each file write/patch, file state captured as checkpoint
- Undo/rewind operations restore files to earlier turn
- Checkpoint stack accessible via agent API

**Strength:**
- Fine-grained file-level rollback
- Turn-aware: can rewind to any prior turn, not just last edit
- Decoupled from session state (file snapshots independent of conversation)

**Gap:** Checkpoints are **file-only**. No checkpoint of LLM reasoning, approval decisions, or tool strategy. If agent made wrong architectural choice, checkpoint can't undo it.

### 3.3 Context Compaction (Token Budget Management)

**Component:** `internal/agent/context.go` (ContextManager)

**Mechanism:**
- Proactive trigger at 95% of effective window; hard block at 98%
- Strategy chain: tool result clearing → summarization (optional) → truncation (fallback)
- Strategies configured via `CompactionStrategy` interface

**Strength:**
- Tiered compaction: lighter strategies run first, heavier ones as fallback
- Signals-aware: `ComputeConversationSignals` classify message importance (HIGH/MEDIUM/LOW)
- Explicit budget tracking: system prompt, tool descriptions, conversation, skill prompts separated

**Weakness:**
- **Compaction is invisible to LLM:** Model doesn't know context was truncated or summarized; token budget is harness-internal
- **Summarization lacks quality tuning:** LLMSummarizer imports importance tags but doesn't expose summary quality metrics or allow LLM to refuse low-confidence summaries
- **No feedback on compaction impact:** If truncation drops important context, agent doesn't learn

**Gap:** Compaction is a **feedback control** (measures tokens, adjusts), but the LLM has no signal. Agent can't reason about confidence in summarized context.

---

## 4. Detailed Strengths (Aligned with Harness Principles)

### 4.1 Correctness > Performance (Enforced)
- CLAUDE.md mandates correctness-first culture
- Performance claims require benchmark validation (ADR-006, ADR-007 decisions backed by concrete analysis)
- TDD discipline ensures no speculative optimization

### 4.2 Keep Quality Left
- Pre-tool approval gates (rule-based trust evaluation)
- Type system boundary checks (interfaces validate at composition time)
- Linter gates block commits before PR submission
- Static security scanners run before expensive LLM analysis

### 4.3 Reduce Variety (Technology Topology Commitment)
- Go language + Charm TUI + Tree-sitter + Starlark + custom HTTP providers (no vendor SDKs)
- Clear tech decisions documented in ADRs
- Reduces architectural drift and cognitive load

### 4.4 Explicit Dependencies & Interfaces
- Tool layer: `Tool` interface with `Name()`, `Description()`, `InputSchema()`, `Execute()`
- Provider layer: `LLMProvider` interface, concrete implementations (Anthropic, OpenAI, Ollama, Z.ai)
- Pipeline middleware: composable `Middleware` functions wrap `HandlerFunc`

### 4.5 Small, Focused Methods & Single Responsibility
- Agent options pattern (`AgentOption` functional options)
- Middleware composition over monolithic execution
- Separate concerns: approval (trust rule), execution (pipeline), persistence (store), hooks (user automation)

---

## 5. Detailed Gaps (Misaligned with Harness Principles)

### 5.1 **GAP 1: No Systematic Evaluator Phase**

**Problem:** Agent generates and executes without intermediate skeptical assessment.

**Current:** Generate (LLM) → Execute (tools) → Loop

**Principle:** Anthropic recommends: Generate → Evaluate (skeptical assessment) → Plan adjustment → Execute

**Impact:**
- Agent can't self-correct based on reasoning inconsistency
- Verification verdicts are collected but ignored in next turn
- Subagents are spawned without explicit success/failure criteria

**Recommendation:**
- Insert `EvaluatorPhase` after LLM response, before tool execution
- Evaluator checks: Does response match user intent? Are tool calls coherent? Are there logical fallacies?
- Evaluator can request regeneration (send back to LLM) or proceed with warnings
- Integrate verification verdicts into evaluator decision

**Implementation Priority:** HIGH

---

### 5.2 **GAP 2: Context Compaction Opaque to LLM**

**Problem:** Token budget is enforced silently; LLM doesn't know context was truncated/summarized.

**Current:** If compaction happens, message history is modified, but LLM receives no notification.

**Impact:**
- LLM may reference details that were compacted away (confabulation risk)
- Agent can't reason about confidence in summarized context
- Summarization quality is not visible to LLM; model can't reject low-confidence summaries

**Recommendation:**
- Add an optional system message: `[CONTEXT COMPACTION] Previous conversation was summarized for brevity. Summarization is a lossy process. If you need details, call the read_result tool or ask user for clarification.`
- Return compaction metadata in `CompactResult`: which strategies ran, token reduction achieved
- Modify `CompletionRequest` to carry context confidence: `{confidence: 0.95, compaction_strategies: ["summarization"]}` 
- Allow LLM to call `request_full_context()` tool if summary is insufficient

**Implementation Priority:** MEDIUM

---

### 5.3 **GAP 3: Security Engine Disconnected from Agent Loop**

**Problem:** Security analysis runs offline (headless code-review mode) or manually; not integrated into interactive agent turns.

**Current:** 
- Security engine invoked only in specific headless modes
- Findings collected but not fed back to agent planning
- No automatic re-scan after tool execution

**Impact:**
- Interactive agent may not discover security issues until after dangerous tools execute
- Verification snapshot contains security verdict, but agent can't see it
- Skills can't react to security findings (e.g., tighten rules, request user confirmation)

**Recommendation:**
- Integrate security scanner into agent turn loop (optional, configurable)
- **Phase-in strategy:**
  - Turn 1: Agent generates plan + tool calls
  - Turn 2 (new): Security engine scans proposed tools/files. If HIGH-severity finding, return verdict to agent before execution
  - Turn 3: Agent reviews verdict and refines plan or proceeds with acknowledgment
- Add `SecurityVerdictEvent` to event stream (alongside `ToolCallEvent`, `ToolResultEvent`)
- Allow skills to hook on `HookOnSecurityVerdict` phase

**Implementation Priority:** MEDIUM → HIGH (if interactive security approval is desired)

---

### 5.4 **GAP 4: Multi-Agent Separation is Tool-Centric, Not Behavior-Centric**

**Problem:** Subagents are spawned to delegate *tools*, not to decompose *tasks* into semantic units.

**Current:** 
- `subagent(task, ...)` tool calls a child agent
- Child inherits tool registry; parent resumes when done
- No explicit success criteria or behavioral contract

**Impact:**
- Subagents can't be optimized for specific task types (e.g., "linter" subagent tuned for code quality checks)
- No planner → multiple agents pattern; just tool delegation
- Subagent failures don't trigger fallback strategies

**Recommendation:**
- Introduce **task types** that match agent behavior:
  - `TaskType::Plan` → planner agent (models, breaks complex task into steps)
  - `TaskType::Generate` → generator agent (code/text synthesis)
  - `TaskType::Verify` → evaluator agent (skeptical assessment, test execution)
  - `TaskType::Refine` → refinement agent (quality improvement, stylistic fixes)
- When spawning subagent, specify task type + context (e.g., `SpawnSubagent(Plan, "design a REST API for user auth")`)
- Subagent behavior tuned to task type (evaluator gets verification-focused tools; planner gets design/search tools)
- Parent receives structured result: `{status: "passed"/"failed", evidence: {...}, next_steps: [...]}`

**Implementation Priority:** LOW → MEDIUM (longer-term architectural improvement)

---

### 5.5 **GAP 5: Feedback Loop from Verification to Planning**

**Problem:** Verification snapshot is collected but doesn't influence subsequent planning.

**Current:**
- Headless runner builds evidence summary (passed/failed verdict)
- Verdict returned in `RunResult` but not fed back to agent
- Next turn starts fresh; agent doesn't know prior turns failed verification

**Impact:**
- Agent can't learn from failed verification attempts
- Same mistake repeated across turns if context is compacted
- "Verified working" state not stored; agent treats all code equally

**Recommendation:**
- **Extend session state** to include verification history:
  ```
  type VerificationRecord {
    turn_number: int,
    verdict: "passed" | "failed",
    reason: string,
    timestamp: time.Time,
  }
  ```
- Store verification records in session persistence layer (SQLite)
- On resume or next turn, load recent verification history
- Inject summary into system prompt: `"Last 3 turns: Turn 1 failed (reason), Turn 2 passed, Turn 3 pending"`
- Allow agent to call `get_verification_history()` tool to explore past verdicts
- Modify plan generation: if recent turns failed same aspect, agent gets suggestion to try alternative approach

**Implementation Priority:** MEDIUM

---

### 5.6 **GAP 6: Tool Input Validation Missing from Approval Flow**

**Problem:** Approval rules only match patterns; they don't validate tool input against schema before execution.

**Current:**
- TrustRuleChecker matches regex against string values in JSON
- Once approved, tool input is passed directly to handler (no schema validation)
- Handler may receive malformed input; error happens late

**Impact:**
- Dangerous inputs can be approved if they match a pattern but are invalid (e.g., `{"timeout_ms": "abc"}` matches string pattern but breaks handler)
- Approval decision is made before input validation; can't reject on schema grounds

**Recommendation:**
- Insert schema validation **before** approval evaluation:
  - Tool definition includes InputSchema (JSON schema)
  - Before TrustRuleChecker runs, validate input against schema
  - If schema invalid, short-circuit approval with error (don't ask user to approve broken input)
- Modify `Tool` interface to expose schema in structured form (currently only string)
- ApprovalChecker receives validated input guarantee

**Implementation Priority:** MEDIUM

---

### 5.7 **GAP 7: Diff Tracking is Turn-Local, Not Multi-Turn Aware**

**Problem:** DiffTracker summarizes changes within a turn but doesn't track cumulative diff across turns.

**Current:**
- `WithDiffTracker()` option attaches tracker to agent
- Tracker summarizes changes in `doneDiffSummary` event
- Reset at turn start; previous turn's diff lost

**Impact:**
- Agent can't see cumulative changes across full session (e.g., "50 lines added across 3 turns")
- Evaluation of "did this turn make progress?" requires manual inspection
- Verification logic in headless runner manually scans tool calls; could be simpler with cumulative diff

**Recommendation:**
- Extend `DiffTracker` to maintain session-level cumulative diff
- Store diff metadata in session (SQLite)
- Load cumulative diff on resume; layer new changes on top
- Inject into system prompt: `"Cumulative changes so far: +150 lines, -30 lines, 5 files modified"`
- Allow agent to call `show_cumulative_diff()` tool

**Implementation Priority:** LOW → MEDIUM

---

## 6. Maturity Assessment (Fowler Framework)

| Aspect | Maturity | Rating | Reasoning |
|--------|----------|--------|-----------|
| **Maintainability** | ★★★★★ | Mature | Type system, interfaces, clear separation, TDD, >90% coverage |
| **Architecture Fitness** | ★★★★☆ | Mature | ADRs guide decisions; tech topology committed; modular composition |
| **Behavior** | ★★★☆☆ | Developing | Verification verdicts exist but disconnected; no systematic evaluation; feedback loops incomplete |

**Overall:** Rubichan is **architecture-fit** but **behavior-immature**. The harness is solid (tests, types, interfaces) but the agent's learning & adaptation loops are weak.

---

## 7. Implementation Roadmap

### Phase 1 (Immediate): Keep Quality Left ✓ (STRONG)
- [x] Approval system (rules-based trust)
- [x] Static security scanners
- [x] Test coverage gates
- [x] Linter gates

### Phase 2 (Next Sprint): Connect Feedback Loops (HIGH PRIORITY)

**Objective:** Make verification verdicts and security findings visible to agent planning.

1. **Evaluator Phase (Gap 5.1)**
   - Add `EvaluatorPhase` between LLM response and tool execution
   - Evaluator checks: coherence, intent match, fallacy detection
   - Can request regeneration or proceed with confidence score
   - **Effort:** 2–3 days
   - **Files:** New package `internal/agent/evaluator.go`; wire into agent turn loop
   
2. **Verification History (Gap 5.5)**
   - Extend session storage: add `verification_records` table
   - Load history on resume
   - Inject into system prompt
   - **Effort:** 1–2 days
   - **Files:** Extend `internal/store/store.go`; modify agent system prompt assembly

3. **Security Feedback (Gap 5.3)**
   - Optional security scan in agent turn loop (configurable)
   - Return verdict event before tool execution
   - Allow agent to refine plan based on security findings
   - **Effort:** 2–3 days
   - **Files:** Wire security engine into `agent.go` turn loop; add event type

### Phase 3 (Later): Improve Context Signaling (MEDIUM)

**Objective:** Make LLM aware of context constraints and compaction.

1. **Compaction Transparency (Gap 5.2)**
   - Add system message when compaction occurs
   - Expose confidence scores
   - Allow LLM to request full context via tool
   - **Effort:** 1 day

2. **Input Validation in Approval (Gap 5.6)**
   - Move schema validation before approval check
   - Reject invalid inputs early
   - **Effort:** 1 day

### Phase 4 (Future): Multi-Agent Decomposition (LOW PRIORITY)

**Objective:** Implement planner/generator/evaluator as separate agent types.

1. **Task-Aware Subagents (Gap 5.4)**
   - Define TaskType enum (Plan, Generate, Verify, Refine)
   - Tune agent behavior per task type
   - Implement behavior contracts
   - **Effort:** 1–2 weeks (substantial)

2. **Cumulative Diff Tracking (Gap 5.7)**
   - Extend DiffTracker to session-level
   - Inject into prompts
   - **Effort:** 2–3 days

---

## 8. Recommendations Summary

### Quick Wins (1–2 days each)
1. Add `[CONTEXT COMPACTION]` system message when summarization runs
2. Store verification history in session; inject summary into system prompt
3. Move input schema validation before approval check

### Medium Effort (2–3 days each, HIGH IMPACT)
1. **Implement evaluator phase** in agent turn loop
2. **Wire security engine** into interactive turn loop with feedback
3. Extend system prompt assembly to expose budget/compaction metadata

### Strategic (1–2 weeks, enables future patterns)
1. Introduce task-aware subagents (planner vs. generator vs. evaluator roles)
2. Implement behavior contracts for multi-agent coordination
3. Add cross-session learning via memory store that reacts to verification history

---

## 9. Conclusion

Rubichan has a **solid, mature harness foundation** (tests, types, approval rules, static checks). The architecture is well-structured and disciplined.

However, the **agent's decision-making loop is largely opaque to its own feedback**. Verification verdicts are collected but ignored. Security findings are silenced. Token compaction happens silently. There's no evaluator phase to skeptically assess before commitment.

**The key insight:** Rubichan is an excellent **platform** (harness is architectural-fit), but not yet an excellent **agent** (behavior is immature). Connecting the feedback loops — making verification visible, security actionable, and context constraints transparent — would elevate it from a sophisticated tool to a genuinely learning system.

**Priority ranking:**
1. **Evaluator phase** — enables skeptical assessment before execution (HIGH IMPACT)
2. **Verification feedback** — closes the learning loop (HIGH IMPACT)
3. **Security integration** — makes safety findings actionable (MEDIUM-HIGH)
4. **Context transparency** — signals constraints to LLM (MEDIUM)
5. **Multi-agent decomposition** — long-term architectural evolution (STRATEGIC)

