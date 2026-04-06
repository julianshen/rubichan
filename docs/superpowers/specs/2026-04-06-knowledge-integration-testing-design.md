# Design: Knowledge Graph End-to-End Integration Testing

> **Goal:** Validate the complete knowledge graph workflow (ingest → index → query → agent integration) across 20+ comprehensive scenarios using real project fixtures, covering happy paths, error handling, scale testing, and recovery.

---

## 1. Architecture Overview

**Five layer-based test suites** covering the knowledge graph stack:

```
┌─────────────────────────────────────────────────────┐
│ Agent Integration Tests                             │
│ (knowledge injection, selector behavior, metrics)   │
├─────────────────────────────────────────────────────┤
│ Query Layer Tests                                   │
│ (selection, ranking, filtering, error handling)     │
├─────────────────────────────────────────────────────┤
│ Index Layer Tests                                   │
│ (schema, rebuild, corruption, recovery)             │
├─────────────────────────────────────────────────────┤
│ Ingest Layer Tests                                  │
│ (LLM, git, file, YAML ingestion)                    │
├─────────────────────────────────────────────────────┤
│ Bootstrap Layer Tests                               │
│ (detection, profile generation, entity writing)     │
├─────────────────────────────────────────────────────┤
│ Shared Test Fixtures & Utilities                    │
│ (real projects, temp dirs, assertions)              │
└─────────────────────────────────────────────────────┘
```

**Key principle:** Each layer is independently testable but validated in integration context using real project fixtures.

---

## 2. Test Organization

### File Structure
```
internal/knowledgegraph/
├── bootstrap_integration_test.go      (2-3 scenarios)
├── ingest_integration_test.go         (4-5 scenarios)
├── index_integration_test.go          (3-4 scenarios)
├── query_integration_test.go          (4-5 scenarios)
└── agent_integration_test.go          (3-4 scenarios)

testdata/
├── go-project/                        (Small Go CLI project)
│   ├── README.md
│   ├── pkg/
│   │   ├── parser.go
│   │   └── renderer.go
│   ├── cmd/
│   │   └── main.go
│   └── .knowledge/
│       ├── architecture.md
│       ├── decisions.md
│       └── .index.db (auto-created by tests)
├── python-project/                    (Python package)
│   ├── README.md
│   ├── src/
│   │   ├── __init__.py
│   │   └── core.py
│   └── .knowledge/
│       └── .index.db
└── mixed-project/                     (Multiple languages, edge cases)
    └── .knowledge/
```

### Test Utilities (Shared Across All Suites)

**File:** `internal/knowledgegraph/test_helpers.go`

```go
// NewTestFixture creates an isolated test environment with temp dir + graph
func NewTestFixture(t *testing.T, projectName string) *TestFixture {
    tmpDir := t.TempDir()
    // Copy testdata/projectName/* to tmpDir
    // Initialize graph
    return &TestFixture{dir, graph}
}

// TestFixture holds temp directory + initialized knowledge graph
type TestFixture struct {
    Dir   string
    Graph KnowledgeGraph
}

// Cleanup removes temp directory
func (f *TestFixture) Cleanup() { ... }

// AssertEntityExists verifies entity is in graph with expected fields
func AssertEntityExists(t *testing.T, g KnowledgeGraph, id, kind, body string) { ... }

// AssertQueryReturns verifies query returns expected entities
func AssertQueryReturns(t *testing.T, g KnowledgeGraph, query string, expectedIDs []string) { ... }

// AssertIndexValid checks schema, foreign keys, statistics
func AssertIndexValid(t *testing.T, dbPath string) error { ... }

// AssertErrorContains validates error messages
func AssertErrorContains(t *testing.T, err error, substring string) { ... }
```

---

## 3. Test Scenarios by Layer (20+ Total)

### 3.1 Bootstrap Layer (2-3 scenarios)

**File:** `bootstrap_integration_test.go`

| # | Scenario | Input | Expected Outcome | Error Handling |
|---|----------|-------|------------------|---|
| 1 | Detect new project | Fresh Go project | Generates BootstrapProfile with language=go, frameworks=[cobra] | - |
| 2 | Generate bootstrap entities | Profile + LLM response | Creates 5-10 initial entities (architecture, decisions) | Handles LLM timeout → skip bootstrap |
| 3 | Handle existing .knowledge/ | Project with existing entities | Skips bootstrap, reports "already initialized" | - |

### 3.2 Ingest Layer (4-5 scenarios)

**File:** `ingest_integration_test.go`

| # | Scenario | Input | Expected Outcome | Error Handling |
|---|----------|-------|------------------|---|
| 4 | LLM ingest from text | Text file + LLM | Extracts 5+ valid entities | LLM timeout → error; invalid JSON → skip entity |
| 5 | Git ingest from history | Git repo (last week) | 3+ decision entities from commits | No git repo → error; empty history → empty result |
| 6 | File ingest from markdown | .knowledge/*.md with YAML frontmatter | Entities from frontmatter + file content | Invalid YAML → error with line number |
| 7 | Manual YAML ingest | entities.yaml file | Validates schema, creates entities | Missing required field → error |
| 8 | Batch ingest (multiple sources) | Multiple ingest types | All entities indexed together | One source fails → continue others |

### 3.3 Index Layer (3-4 scenarios)

**File:** `index_integration_test.go`

| # | Scenario | Input | Expected Outcome | Error Handling |
|---|----------|-------|------------------|---|
| 9 | Index creation | 10 entities | SQLite index created with correct schema | Disk full → error |
| 10 | Reindex from entities | Index + entities on disk | Index matches entities, stats recalculated | Corrupted entity file → skip file |
| 11 | Corruption detection | Manually corrupted .index.db (truncate file to 100 bytes) | Detected on open, triggers rebuild | Rebuild succeeds, old index removed |
| 12 | Recovery from corruption | Corrupted index + valid entities on disk | Rebuilds index from entities in .knowledge/ | Entities also corrupted → rebuild fails with clear error |

### 3.4 Query Layer (4-5 scenarios)

**File:** `query_integration_test.go`

| # | Scenario | Input | Expected Outcome | Error Handling |
|---|----------|-------|------------------|---|
| 13 | Query by kind | kind=architecture | Returns all architecture entities | Invalid kind → empty result |
| 14 | Query by layer | layer=ingestor | Returns entities tagged for ingestor layer | Missing layer → empty result |
| 15 | Ranking by confidence | 5 entities with varying confidence | Ranked by confidence descending | All low confidence → still returned in order |
| 16 | Ranking by usage count | 5 entities with varying usage | Ranked by usage descending | Zero usage → ranked by confidence | 
| 17 | Empty query result | Query matching no entities | Returns empty slice, no error | - |

### 3.5 Agent Integration Layer (3-4 scenarios)

**File:** `agent_integration_test.go`

| # | Scenario | Input | Expected Outcome | Error Handling |
|---|----------|-------|------------------|---|
| 18 | Knowledge injected in prompt | Agent with graph + query | System prompt includes selected entities | Graph unavailable → prompt unchanged |
| 19 | SelectByScore returns relevant | Agent with ContextSelector | Entities relevant to current message ranked first | Selector returns error → skip injection |
| 20 | RecordUsage increments metrics | Agent selects 5 entities | entity_stats.usage_count incremented for each | RecordUsage error → silently ignored |
| 21 | Selector graceful degradation | Agent with no knowledge graph | Agent works normally, no knowledge injection | - |

---

## 4. Test Fixtures (Real Projects)

### 4.1 Go Project (`testdata/go-project/`)

Small but realistic Go CLI project:
```
.
├── README.md (documents project purpose)
├── go.mod
├── cmd/cli/main.go
├── pkg/
│   ├── parser/parser.go (file parsing logic)
│   └── renderer/renderer.go (output rendering)
├── .knowledge/
│   ├── architecture.md (YAML frontmatter: kind, layer, body)
│   ├── decisions.md (ADR-style: why this approach)
│   └── gotchas.md (known pitfalls)
└── .agent.md (project-specific context)
```

### 4.2 Python Project (`testdata/python-project/`)

Different language, different structure:
```
.
├── README.md
├── setup.py
├── src/
│   ├── __init__.py
│   └── core.py
├── tests/
│   └── test_core.py
└── .knowledge/
    ├── module_guide.md
    └── integration_points.md
```

### 4.3 Mixed Project (`testdata/mixed-project/`)

Edge cases: multiple languages, missing files, incomplete docs:
```
.
├── README.md (minimal)
├── go.mod / package.json / Pipfile
├── src/ (mixed languages)
├── docs/ (partial documentation)
└── .knowledge/
    ├── incomplete.md (missing body)
    ├── broken.yaml (invalid syntax)
    └── architecture.md (valid)
```

---

## 5. Success Criteria & Performance Thresholds

### Correctness
- ✅ Bootstrap detects language accurately (go, python, javascript, rust)
- ✅ Ingest creates valid entities with all required fields
- ✅ Query returns expected entities in correct order
- ✅ Knowledge injected into agent prompts with correct context

### Error Handling
- ✅ Invalid YAML → specific error message + line number
- ✅ Missing file → error + suggestion
- ✅ LLM timeout → graceful skip
- ✅ Index corruption → automatic rebuild
- ✅ Query on empty graph → empty result (not error)
- ✅ Unavailable graph → agent continues without knowledge

### Performance Thresholds
- ✅ 100-entity queries: <100ms
- ✅ Ingest 50 entities: <5s
- ✅ Reindex full graph: <10s
- ✅ Memory usage: <50MB for 500 entities

### Data Integrity
- ✅ Index matches entity files (all entities indexed)
- ✅ Reindex produces identical results (deterministic)
- ✅ RecordUsage updates persist (survives process restart)
- ✅ Confidence scores persist in SQLite

---

## 6. Test Execution Strategy

### Local Development
Run all suites independently:
```bash
go test ./internal/knowledgegraph -run Bootstrap  # Just bootstrap tests
go test ./internal/knowledgegraph -run Ingest     # Just ingest tests
go test ./internal/knowledgegraph -run Integration # All integration tests
```

### Parallel Execution
Tests are isolated (temp dirs), so suites can run in parallel:
```bash
go test -p 4 ./internal/knowledgegraph  # Run 4 packages in parallel
```

### Real Project Fixtures
All tests copy `testdata/` projects to temp directories:
- No modification of original fixtures
- Each test gets clean state
- Can run tests repeatedly without side effects

---

## 7. Implementation Notes

### Shared Setup (test_helpers.go)
- `NewTestFixture()` — Creates isolated test environment
- `AssertEntityExists()` — Validates entity in graph
- `AssertQueryReturns()` — Validates query results
- `AssertIndexValid()` — Validates database schema
- Error assertion helpers for consistent error testing

### Test Data
- Fixtures stored in `testdata/` alongside tests
- Each fixture is self-contained (has README explaining scenario)
- Fixtures committed to git (keep small, <100KB total)

### Mocking Strategy
- **Real:** File I/O, SQLite, knowledge graph operations
- **Mock:** LLM completer (returns pre-defined entities: {"id": "arch-001", "kind": "architecture", "body": "MVC pattern", "confidence": 0.9})
- **Stub:** Git operations (use committed fixture history: 5-10 commits with dates from last 2 weeks)

### No External Dependencies
- Tests use offline LLM mock (no API calls)
- No network access required
- No external databases
- ~30 second total runtime (all 20+ scenarios)

---

## 8. Success Outcomes

After implementation, the test suite will:
1. **Validate correctness** — Knowledge graph workflow works end-to-end
2. **Catch regressions** — Changes that break knowledge integration
3. **Document behavior** — Tests serve as usage examples
4. **Build confidence** — Developers can refactor knowing tests catch issues
5. **Enable future features** — Tests provide foundation for search overlay, interactive shell

