# Knowledge Graph Test Architecture

This document describes the test structure for the knowledge graph subsystem, serving as a guide for understanding, extending, and maintaining the test suite.

## Overview

The knowledge graph testing suite uses a **layer-based architecture** that mirrors the system under test. Each layer is independently testable but validated in integration context using real project fixtures.

```
┌─────────────────────────────────────────────────────┐
│ Agent Integration Tests (6 tests)                   │
│ (knowledge injection, selector behavior, metrics)   │
├─────────────────────────────────────────────────────┤
│ Query Layer Tests (11 tests)                        │
│ (filtering, ranking, error handling)                │
├─────────────────────────────────────────────────────┤
│ Index Layer Tests (5 tests)                         │
│ (schema, rebuild, corruption, recovery)             │
├─────────────────────────────────────────────────────┤
│ Ingest Layer Tests (8 tests)                        │
│ (LLM, git, file, YAML ingestion)                    │
├─────────────────────────────────────────────────────┤
│ Bootstrap Layer Tests (3 tests)                     │
│ (detection, profile generation)                     │
├─────────────────────────────────────────────────────┤
│ Shared Test Fixtures & Utilities (6 tests)          │
│ (fixture creation, assertions, directory copy)      │
└─────────────────────────────────────────────────────┘
```

## File Organization

### Test Files

```
internal/knowledgegraph/
├── test_helpers.go                 # Shared utilities (NewTestFixture, assertions)
├── test_helpers_test.go            # Tests for helpers
├── bootstrap_integration_test.go    # Bootstrap layer tests
├── ingest_integration_test.go       # Ingest layer tests
├── index_integration_test.go        # Index layer tests
├── query_integration_test.go        # Query layer tests
└── agent_integration_test.go        # Agent integration tests
```

### Fixtures

```
testdata/
├── go-project/                  # Go CLI project fixture
│   ├── README.md
│   ├── go.mod
│   ├── cmd/cli/main.go
│   ├── pkg/parser/parser.go
│   ├── pkg/renderer/renderer.go
│   └── .knowledge/
│       ├── architecture.md      # YAML frontmatter format
│       ├── decisions.md
│       └── gotchas.md
├── python-project/              # Python package fixture
│   ├── README.md
│   ├── setup.py
│   ├── src/
│   └── .knowledge/
│       ├── module_guide.md
│       └── integration_points.md
└── mixed-project/               # Multi-language fixture with edge cases
    ├── README.md
    ├── go.mod
    ├── package.json
    ├── Pipfile
    └── .knowledge/
        ├── incomplete.md        # Empty body (edge case)
        ├── broken.yaml          # Invalid syntax (error case)
        └── architecture.md
```

## Key Design Patterns

### 1. Real Fixtures vs. Mocks

**Pattern:** Use real project structures from `testdata/` instead of mocked data.

**Rationale:** 
- Tests against actual YAML structures catch serialization/deserialization bugs
- Real `.knowledge/` layouts reveal organizational issues
- Discovered that fixture has more entities than tests expected

**Example:**
```go
// Good: Real fixture with actual structure
fixture := NewTestFixture(t, "go-project")
graph := fixture.Graph

// Results reflect real `.knowledge/` contents
entities, _ := graph.List(context.Background(), ...)
```

### 2. Test Isolation via Temp Directories

**Pattern:** Each test gets its own isolated environment via `t.TempDir()`.

**Rationale:**
- No test order dependencies
- Tests can run in parallel
- Automatic cleanup (no manual teardown)

**Implementation:**
```go
// NewTestFixture creates isolated environment
func NewTestFixture(t *testing.T, projectName string) *TestFixture {
    tmpDir := t.TempDir()  // Auto-cleaned after test
    copyDir(fixtureSource, tmpDir)  // Copy fixture
    g, _ := OpenGraph(context.Background(), tmpDir)
    return &TestFixture{Dir: tmpDir, Graph: g}
}
```

### 3. Deterministic Mocks

**Pattern:** Mock LLM returns fixed, deterministic JSON responses.

**Rationale:**
- Tests reproducible locally (zero external API dependencies)
- No flaky tests from network timeouts
- Fast execution (no API calls)

**Example:**
```go
type mockCompleter struct{}

func (m *mockCompleter) Complete(ctx context.Context, prompt string) (string, error) {
    // Always return same entities, regardless of input
    return `{
        "entities": [
            {
                "id": "llm-arch-001",
                "kind": "architecture",
                "confidence": 0.9,
                "body": "Test architecture"
            }
        ]
    }`, nil
}
```

### 4. Graceful Degradation Testing

**Pattern:** Test systems without required components using NullSelector/stub implementations.

**Rationale:**
- Validates that agent continues without knowledge graph
- Ensures no hard dependencies on optional subsystems
- Demonstrates proper error handling

**Example:**
```go
// Test that agent works without graph
selector := NewNullSelector()
results, err := selector.Select(ctx, "query", 5)
// Should succeed with empty results, not error
```

### 5. Layer Isolation with Integration Context

**Pattern:** Test layers independently but validate integration via real graph and selectors.

**Rationale:**
- Catches bugs at layer boundaries
- Isolates failures to specific layer
- Validates actual integration path

**Example:**
```go
// Test Index layer independently
func TestIndex_RebuildFromEntities(t *testing.T) {
    fixture := NewTestFixture(t, "go-project")
    graph := fixture.Graph.(*KnowledgeGraph)  // Real graph
    
    // Test rebuild method in isolation
    err := graph.RebuildIndex(context.Background())
    require.NoError(t, err)
    
    // But validate against real schema
    AssertIndexValid(t, dbPath)
}
```

## Shared Test Utilities

### NewTestFixture(t, projectName)
Creates isolated test environment with copied fixture and initialized graph.

```go
fixture := NewTestFixture(t, "go-project")
defer fixture.Cleanup()

// fixture.Dir: temp directory with fixture copy
// fixture.Graph: initialized KnowledgeGraph
```

### copyDir(src, dst)
Recursively copies directory tree with proper error handling.

**Used by:** NewTestFixture, test setup

### AssertEntityExists(t, g, id, kind, bodyPrefix)
Verifies entity exists in graph with expected fields.

**Usage:**
```go
AssertEntityExists(t, graph, "arch-001", "architecture", "MVC pattern")
```

### AssertQueryReturns(t, g, query, expectedIDs)
Validates query returns expected entities.

**Usage:**
```go
AssertQueryReturns(t, graph, "", expectedIDs)
```

### AssertIndexValid(t, dbPath)
Checks SQLite schema, tables, foreign keys, required columns.

**Usage:**
```go
err := AssertIndexValid(t, filepath.Join(dir, ".knowledge", ".index.db"))
require.NoError(t, err)
```

### AssertErrorContains(t, err, substring)
Validates error message contains expected text.

**Usage:**
```go
AssertErrorContains(t, err, "invalid YAML")
```

## Running Tests

### Run All Integration Tests
```bash
go test ./internal/knowledgegraph -run "TestNewTestFixture|TestAssert|TestBootstrap|TestIngest|TestIndex|TestQuery|TestAgent" -v
```

### Run Specific Layer
```bash
go test ./internal/knowledgegraph -run "TestQuery" -v
go test ./internal/knowledgegraph -run "TestIngest" -v
```

### Run with Coverage
```bash
go test ./internal/knowledgegraph -cover
```

### Run Single Test
```bash
go test ./internal/knowledgegraph -run "TestQuery_RankingByConfidence" -v
```

## Test Data Guide

### Entity Frontmatter Format

Tests use YAML frontmatter in markdown files:

```yaml
---
id: arch-001
kind: architecture
layer: base
confidence: 0.9
title: Architecture Pattern
tags: [mvc, layered]
source: manual
---

# Architecture

Description content goes here.
```

**Required Fields:**
- `id`: Unique identifier
- `kind`: Entity type (architecture, decision, pattern, module, integration, gotcha)
- `layer`: Scope (base, team, session; default: base)
- `confidence`: Score 0.0-1.0 (default: 0.0)

**Optional Fields:**
- `title`: Human-readable name
- `tags`: Array of labels
- `source`: Where entity came from (manual, llm, git, file)

### Creating Test Fixtures

To add a new test fixture:

1. Create directory: `testdata/my-project/`
2. Add project structure: `README.md`, language files (`.go`, `.py`, etc.)
3. Create `.knowledge/` with YAML markdown files
4. Add to test: `fixture := NewTestFixture(t, "my-project")`

## Bug Fixes Discovered During Testing

### 1. SQLite Connection Pool Deadlock
**File:** `graph.go`, line 135  
**Issue:** MaxOpenConns(1) caused concurrent Get() calls to deadlock  
**Fix:** Increased to MaxOpenConns(25)

### 2. Empty Query FTS5 Syntax Error
**File:** `graph.go`, line 517  
**Issue:** Empty query strings passed to FTS5 MATCH operator (invalid)  
**Fix:** Added explicit empty query handling using List()

Both bugs were caught by integration tests before reaching production.

## Extending Tests

### Adding a New Integration Test

1. **Choose layer** (Bootstrap, Ingest, Index, Query, Agent)
2. **Create test function** following naming pattern: `TestLayerName_Scenario`
3. **Use fixture**: `fixture := NewTestFixture(t, "go-project")`
4. **Write test** using layer-specific assertions
5. **Run**: `go test ./internal/knowledgegraph -run "TestLayerName" -v`

**Template:**
```go
func TestQuery_NewScenario(t *testing.T) {
    fixture := NewTestFixture(t, "go-project")
    defer fixture.Cleanup()

    graph := fixture.Graph

    // Setup
    entity := &kg.Entity{
        ID:         "test-001",
        Kind:       kg.KindPattern,
        Title:      "Test Pattern",
        Body:       "Test content",
        Confidence: 0.9,
    }
    require.NoError(t, graph.Put(context.Background(), entity))

    // Test
    results, err := graph.Query(context.Background(), kg.QueryRequest{
        Text:  "pattern",
        Limit: 5,
    })

    // Verify
    require.NoError(t, err)
    require.NotEmpty(t, results)
}
```

### Adding a New Fixture

1. Create `testdata/new-project/` directory
2. Add real project files (README, source code)
3. Create `.knowledge/` with test entities
4. Use in test: `fixture := NewTestFixture(t, "new-project")`

## Performance Expectations

- **Query layer**: <100ms per test (100-entity queries)
- **Ingest layer**: <1s per test (50-entity batch)
- **Full suite**: ~1 second (all 45 tests)
- **No flakes**: Deterministic, no timeouts

## Maintenance

### When Code Changes
Run full test suite: `go test ./internal/knowledgegraph -v`

### When Fixtures Change
Update relevant test assertions if entity counts change

### When Adding Features
Add corresponding integration tests following existing patterns

### When Debugging Failures
Use `-run` flag to isolate test: `go test ./internal/knowledgegraph -run "TestQuery_RankingByConfidence" -v`

---

## References

- **Design Spec:** `docs/superpowers/specs/2026-04-06-knowledge-integration-testing-design.md`
- **Implementation Plan:** `docs/superpowers/plans/2026-04-07-knowledge-integration-testing-implementation.md`
- **Completion Report:** `docs/superpowers/integration-testing-completion.md`
