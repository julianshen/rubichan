# Design: `/initknowledgegraph` Skill — Hybrid Knowledge Graph Bootstrap

**Date:** 2026-04-06  
**Status:** Design approved, ready for implementation  
**Author:** Julian Shen + Claude

---

## Executive Summary

`/initknowledgegraph` is a built-in skill that bootstraps a new project's knowledge graph through a hybrid workflow:

1. **Interactive questionnaire** (skill) collects project context: name, tech stack, architecture style, pain points
2. **Code analysis** (skill) scans codebase for modules, recent architectural decisions, integrations
3. **Entity creation** (skill) writes initial entities to .knowledge/
4. **Interactive refinement** (agent) starts a focused chat to refine, add, or adjust the discovered entities

This design reuses Rubichan's existing ingestion and agent infrastructure, keeping the skill focused on orchestration while leveraging the agent for conversational refinement.

---

## Goals & Success Criteria

**Goals:**
- Enable new projects to bootstrap a knowledge graph in <15 minutes
- Reduce manual data entry by auto-discovering entities from code + git
- Provide a guided, conversational experience (skill + agent collaboration)
- Establish single source of truth for project context early in development

**Success Criteria:**
- ✅ Skill completes questionnaire and analysis without external API calls (LLM not required for bootstrap)
- ✅ Typical project (100-500 files) generates 10-20 initial entities
- ✅ Agent can start with bootstrap context and continue refinement naturally
- ✅ Tests cover happy path, edge cases (empty project, existing graph), and error handling
- ✅ Documented in `rubichan skill list` and `rubichan skill info initknowledgegraph`

---

## Architecture

### Three-Phase Orchestration

```
User: rubichan /initknowledgegraph
  ↓
[PHASE 1: QUESTIONNAIRE]
  Interactive prompts for project profile
  ↓ (BootstrapProfile struct)
[PHASE 2: ANALYSIS]
  Scan codebase: modules, git history, integrations
  ↓ ([]ProposedEntity)
[PHASE 3: CREATION + HANDOFF]
  Write entities to .knowledge/
  Create .bootstrap.json metadata
  Signal agent to start
  ↓
[AGENT: REFINEMENT]
  Chat with developer to refine entities
  ↓
[COMMIT]
  Save final .knowledge/ state
```

### Phase 1: Interactive Questionnaire

**Prompts (in order):**
1. **Project Name** (string)
   - Used as entity ID prefix, e.g., "myapp-arch-*"
   
2. **Primary Tech Stack** (multi-select with examples)
   - Backend: Go, Python, Node.js, Java, Rust, Other
   - Frontend: React, Vue, Svelte, Next.js, Web Components, Other
   - Database: PostgreSQL, MongoDB, Redis, DynamoDB, SQLite, Other
   - Infrastructure: Kubernetes, Docker, AWS, GCP, Azure, Other
   - Example shown: "e.g., Go + React + PostgreSQL + AWS"

3. **Architectural Style** (single choice)
   - Options: Monolithic, Microservices, Serverless, Hybrid
   - Brief description for each

4. **Key Pain Points** (open input, comma-separated)
   - Examples: "scaling, testing, deployment, logging, API design"
   - User describes top 2-3 challenges the project faces

5. **Team Size & Composition** (single choice)
   - Small (1-3 people), Medium (4-10), Large (10+)
   - Mainly: Frontend / Backend / Full-stack / Mixed

6. **Existing Project?** (yes/no)
   - If yes: scope analysis to existing codebase
   - If no: skip file-level analysis, focus on profile + git

**Output:**
```go
type BootstrapProfile struct {
    ProjectName      string
    BackendTechs     []string
    FrontendTechs    []string
    DatabaseTechs    []string
    InfrastructureTechs []string
    ArchitectureStyle string
    PainPoints       []string
    TeamSize         string
    TeamComposition  string
    IsExisting       bool
    CreatedAt        time.Time
}
```

### Phase 2: Code Analysis

**Analysis passes** (parallel where possible):

1. **Module/Package Discovery**
   - Walk source tree (src/, app/, backend/, frontend/, etc.)
   - Identify top-level packages/directories
   - Create `KindModule` entities for each
   - Example: "Auth Package", "Database Layer", "API Router"

2. **Git History Parsing**
   - Last 30 commits (configurable)
   - Search for keywords: "architecture", "decision", "pattern", "refactor", "fix", pain point keywords from profile
   - Create `KindDecision` entities for significant commits
   - Example: "Switched from REST to GraphQL", "Added caching layer"

3. **Integration Point Detection**
   - Parse import statements (Go, Node.js, Python)
   - Identify external libraries + versions
   - Create `KindIntegration` entities
   - Example: "PostgreSQL via GORM", "React for UI", "Redis for caching"

4. **Language-Specific AST Analysis** (if >50% of codebase in single language)
   - For Go: extract main entry points, exported functions
   - For JS/TS: identify React components, main services
   - Create lightweight `KindArchitecture` entities for design patterns found
   - Example: "Dependency injection pattern", "Observer pattern in event system"

**Heuristics:**
- Confidence scoring: 0.9 (high) for discovered modules, 0.7 (medium) for git inferences, 0.5 (exploratory) for AST patterns
- Cap file scanning to top 50 files by size/relevance if codebase >1000 files
- Skip analysis on empty codebase (new project): just use profile to create skeleton entities

**Output:**
```go
type ProposedEntity struct {
    ID           string  // e.g., "myapp-auth-module"
    Kind         kg.EntityKind
    Title        string
    Body         string
    SourceType   string  // "module", "git", "integration", "ast"
    Confidence   float64 // 0.5-0.9
    Tags         []string
}
```

### Phase 3: Entity Creation & Agent Handoff

**Step 1: Write Entities**
- Create each proposed entity as markdown file in .knowledge/
- Path: `.knowledge/{layer}/{kind}/{entity-id}.md`
- Layer: "base" (all entities discovered are base layer, not team/session)
- Frontmatter: ID, Kind, Layer, Title, Tags, Source, Confidence

**Step 2: Create Bootstrap Metadata**
- Write `.knowledge/.bootstrap.json`:
  ```json
  {
    "profile": { /* BootstrapProfile */ },
    "created_entities": ["myapp-auth-module", "myapp-database-layer", ...],
    "analysis_metadata": {
      "modules_found": 5,
      "git_commits_analyzed": 30,
      "integrations_detected": 12,
      "analysis_timestamp": "2026-04-06T..."
    }
  }
  ```

**Step 3: Signal Agent**
- Skill exits cleanly with message:
  ```
  ✅ Bootstrap complete!
  📚 Created 15 entities in .knowledge/
  
  Starting interactive refinement with agent...
  ```
- Agent detects `.bootstrap.json` and enters "bootstrap mode"

**Step 4: Agent Startup with Bootstrap Context**
- Agent reads `.bootstrap.json`
- Generates system prompt prefix:
  ```
  User just bootstrapped a knowledge graph for [ProjectName].
  
  Here's what we discovered together:
  - 5 modules (auth, database, api, ui, config)
  - 3 architectural decisions (GraphQL migration, caching strategy, monorepo choice)
  - 7 integrations (PostgreSQL, Redis, React, Docker, GitHub Actions, etc.)
  
  Let's refine these entities. You can:
  - Ask me to elaborate on any entity
  - Suggest changes or corrections
  - Add entities I might have missed
  - Adjust confidence scores
  
  When you're done, type /done and I'll save the refined knowledge graph.
  ```
- Agent continues normally, with `/done` to finalize

---

## Error Handling & Edge Cases

### Questionnaire Phase
- **User cancels mid-questionnaire** → Exit cleanly, no state written
- **Invalid input** (e.g., empty project name) → Re-prompt with validation message
- **.knowledge/ already exists** → Prompt "Graph exists. Enhance (add to existing) or start fresh?" and branch logic

### Analysis Phase
- **Empty codebase** (new project with no source files) → Skip file analysis, use profile + skeleton entities only
- **Git unavailable** (not a git repo) → Warn, skip git analysis, continue with code analysis
- **Large codebase** (1000+ files) → Cap file scanning to top 50 by relevance, report "Analyzed top 50 files"
- **Unreadable files** (permissions, parse errors) → Log and skip, continue with other files
- **Analysis timeout** (takes >5 min) → Return partial results, report "Analysis timed out. Using findings so far."

### Entity Creation Phase
- **.knowledge/ not writable** → Rollback any written files, clear error message
- **Entity ID collision** with existing entities → Auto-rename with `-2`, `-3` suffix, report renamed entities
- **Proposed entity has empty body** → Include guidance comment: "Fill in why this decision was made"

### Agent Handoff
- **Agent fails to start** → Skill reports success anyway with message: "Entities created in .knowledge/. Run `rubichan` to start interactive refinement."
- **No bootstrap.json found** → Agent starts normally (degraded UX but not a failure)

---

## Integration Points

### With Existing Systems

**knowledgegraph.Ingestor** (reuse):
- Use existing `FileIngestor`, `GitIngestor` infrastructure for analysis
- No changes needed to Ingestor interfaces

**Agent System**:
- Agent reads bootstrap context from optional flag: `--bootstrap-context .knowledge/.bootstrap.json`
- Modify `cmd/rubichan/main.go:runInteractive()` to detect and load bootstrap context
- No changes to agent core logic

**Skill Registry**:
- `/initknowledgegraph` registered as built-in skill (no installation needed)
- Listed in `rubichan skill list` with description
- Available via `skill info initknowledgegraph`

---

## Testing Strategy

### Unit Tests (in `cmd/rubichan/initknowledge_test.go`)

**Questionnaire Validation:**
- `TestBootstrapProfileValidation()` — valid/invalid inputs, required fields
- `TestQuestionnaireFlow()` — mock user responses, verify profile captures correctly
- `TestMultiSelectHandling()` — verify tech stack multi-select merges correctly

**Entity Generation:**
- `TestProposedEntityFromModule()` — module → KindModule entity mapping
- `TestProposedEntityFromGitCommit()` — commit → KindDecision entity extraction
- `TestProposedEntityFromIntegration()` — library import → KindIntegration entity
- `TestConfidenceScoringLogic()` — verify confidence ranges by source type

**Metadata Serialization:**
- `TestBootstrapContextJSON()` — serialize/deserialize bootstrap metadata correctly
- `TestBootstrapProfilePersistence()` — profile survives round-trip

### Integration Tests (in `internal/knowledgegraph/bootstrap_test.go`)

**File System Operations:**
- `TestBootstrapWritesEntityFiles()` — entities appear in .knowledge/ with correct structure
- `TestBootstrapCreatesBootstrapJSON()` — metadata file created with correct contents
- `TestBootstrapRollbackOnWriteFailure()` — partial failure cleanup

**Interaction with Existing Graph:**
- `TestBootstrapWithExistingGraph()` — enhance mode appends without overwriting
- `TestBootstrapDetectsCollisions()` — ID collision detection works
- `TestBootstrapSkipsAnalysisOnEmptyRepo()` — empty project handled gracefully

**Error Paths:**
- `TestBootstrapEmptyCodebase()` — new project with no source files
- `TestBootstrapGitUnavailable()` — non-git-repo graceful degradation
- `TestBootstrapLargeCodebase()` — capping analysis at 50 files

### End-to-End Tests (manual documentation)

Document in `docs/superpowers/examples/initknowledgegraph/`:
- `example-go-project/` — bootstrap a Go project, show resulting entities
- `example-node-project/` — bootstrap a Node.js project
- `example-existing-project/` — bootstrap on existing mature codebase

---

## Implementation Files

**Files to Create:**
- `cmd/rubichan/initknowledge.go` — Skill entry point, questionnaire, orchestration
- `internal/knowledgegraph/bootstrap.go` — Analysis logic, entity generation
- `cmd/rubichan/initknowledge_test.go` — Unit tests for skill
- `internal/knowledgegraph/bootstrap_test.go` — Integration tests
- `docs/superpowers/examples/initknowledgegraph/example-go-project/` — Example walkthrough

**Files to Modify (minor):**
- `cmd/rubichan/main.go` — Add `--bootstrap-context` flag detection in runInteractive()
- `internal/agent/agent.go` — Read bootstrap context, adjust system prompt if present

---

## Success Metrics

After implementation, we'll measure:
1. **Entity quality** — Do generated entities have useful titles/bodies, or do they need heavy editing?
2. **User time saved** — Can a developer bootstrap a knowledge graph in <15 minutes?
3. **Accuracy** — Do discovered entities align with developer's actual project structure?
4. **Completion rate** — Do users finish the full questionnaire → agent refinement, or drop off?

---

## Future Enhancements (Out of Scope)

- LLM-powered entity enhancement (currently skill uses heuristics only)
- Custom analysis plugins (extend what Ingestors can discover)
- Bootstrap templates for common architectures (e.g., "standard microservices")
- API to programmatically trigger bootstrap from other tools
