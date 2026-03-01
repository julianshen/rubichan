# Plan: Progressive Disclosure, Instruction Skills, and Context Budget

Three features from the Codex research analysis, implemented via TDD.

## Feature 1: Progressive Disclosure (SkillIndex)

**Goal**: Introduce a lightweight `SkillIndex` type for efficient system prompt building. Instead of exposing full manifests to the prompt builder, only expose name + description + types (~50 tokens per skill). Full manifest is used only at activation time.

### Tests

- [x] **1.1** `TestSkillIndexFromManifest` — `SkillIndex` is correctly created from a `SkillManifest` with only name, description, types, and triggers copied.
- [x] **1.2** `TestSkillIndexFromManifestPreservesAllFields` — All four fields are populated; zero-value manifest produces zero-value index.
- [x] **1.3** `TestRuntimeGetSkillIndexes` — `Runtime.GetSkillIndexes()` returns indexes for all discovered skills (both active and inactive).
- [x] **1.4** `TestRuntimeGetSkillIndexesEmpty` — Returns empty slice when no skills discovered.
- [x] **1.5** `TestRuntimeGetSkillIndexesReflectsSource` — Each index carries the correct `Source` from discovery.
- [x] **1.6** `TestSkillIndexTypesCopyIsolation` — Types slice is defensively copied to prevent shared mutation.

### Implementation

- Added `SkillIndex` struct to `types.go` (Name, Description, Types, Triggers, Source, Dir)
- Added `NewSkillIndex(manifest *SkillManifest, source Source, dir string) SkillIndex`
- Added `Runtime.GetSkillIndexes() []SkillIndex`

---

## Feature 2: Instruction Skills (SKILL.md)

**Goal**: Support lightweight markdown-only skills. A directory with just a `SKILL.md` file (YAML frontmatter + markdown body) becomes a prompt skill automatically. No SKILL.yaml, no backend, no code.

### Format

```markdown
---
name: react-best-practices
version: 1.0.0
description: React development patterns and conventions
triggers:
  files: ["*.tsx", "*.jsx"]
  languages: [typescript, javascript]
---

## Instructions

When working with React components in this project...
```

### Tests

- [x] **2.1** `TestParseInstructionSkill` — Parse a valid `SKILL.md` file: extracts frontmatter as manifest fields and body as resolved prompt content.
- [x] **2.2** `TestParseInstructionSkillMinimal` — Parse with only required fields (name, version, description); body can be empty.
- [x] **2.3** `TestParseInstructionSkillInvalidFrontmatter` — Missing required fields produce validation errors.
- [x] **2.4** `TestParseInstructionSkillNoFrontmatter` — File without `---` delimiters returns an error.
- [x] **2.5** `TestParseInstructionSkillTypesDefaultToPrompt` — If `types` is omitted, defaults to `[prompt]`.
- [x] **2.6** `TestParseInstructionSkillRejectsNonPromptTypes` — If `types` includes non-prompt types (e.g., tool), returns error (instruction skills are prompt-only).
- [x] **2.7** `TestScanDirDiscoversInstructionSkills` — `scanDir` discovers SKILL.md-only directories alongside SKILL.yaml directories.
- [x] **2.8** `TestScanDirSkillYAMLTakesPrecedence` — When both SKILL.yaml and SKILL.md exist in same directory, SKILL.yaml wins.
- [x] **2.9** `TestDiscoverIntegrationWithInstructionSkills` — Full `Loader.Discover` finds instruction skills and returns them with correct source/dir.
- [x] **2.10** `TestRuntimeActivateInstructionSkill` — Activating an instruction skill wires the markdown body as prompt fragment (no backend needed).
- [x] **2.11** `TestInstructionSkillPromptFragmentContent` — After activation, the `PromptCollector` contains a fragment with the markdown body as `ResolvedPrompt`.

### Implementation

- Added `ParseInstructionSkill(data []byte) (*SkillManifest, string, error)` to `manifest.go` (returns manifest + body)
- Added `splitFrontmatter()` helper for YAML frontmatter extraction
- Added `InstructionBody` field to `DiscoveredSkill` and `Skill`
- Modified `scanDir` to check for `SKILL.md` when `SKILL.yaml` not found
- Modified `wirePromptSkill` in `integration.go` to use `InstructionBody` when available

---

## Feature 3: Context Budget Management

**Goal**: Add a global context budget that limits the total tokens from all skill prompt fragments. When over budget, lower-priority skills are truncated or excluded.

### Tests

- [x] **3.1** `TestNewContextBudget` — `DefaultContextBudget` creates budget with sensible defaults.
- [x] **3.2** `TestContextBudgetSourcePriority` — Sources are ranked: SourceInline > SourceBuiltin > SourceUser > SourceProject > SourceMCP.
- [x] **3.3** `TestPromptCollectorBudgetedFragmentsUnderBudget` — When total tokens are under budget, all fragments are returned unchanged.
- [x] **3.4** `TestPromptCollectorBudgetedFragmentsOverBudget` — When over budget, lowest-priority fragments are excluded first.
- [x] **3.5** `TestPromptCollectorBudgetedFragmentsTruncation` — When a single fragment exceeds per-skill limit, it is truncated.
- [x] **3.6** `TestPromptCollectorBudgetedFragmentsPreservesOrder` — Higher-priority fragments are always included before lower-priority ones.
- [x] **3.7** `TestPromptCollectorBudgetedFragmentsNoBudget` — When no budget is set (nil/zero), all fragments are returned (backward compatible).
- [x] **3.8** `TestRuntimeGetBudgetedPromptFragments` — `Runtime.GetBudgetedPromptFragments()` applies the budget and returns within-budget fragments.
- [x] **3.9** `TestContextBudgetEstimateTokens` — Token estimation function: ~4 chars per token approximation works correctly.
- [x] **3.10** `TestPromptCollectorBudgetedFragmentsPartialFit` — Fragment that partially fits within remaining budget is truncated to fit.

### Implementation

- Added `ContextBudget` struct to `types.go` (MaxTotalTokens, MaxPerSkillTokens)
- Added `DefaultContextBudget()` constructor with sensible defaults (8000 total, 2000 per-skill)
- Added `estimateTokens(s string) int` utility function (~4 chars per token)
- Added `sourceBudgetPriority(Source) int` function (higher = more important for budget)
- Added `Source` field to `PromptFragment`
- Added `PromptCollector.BudgetedFragments(budget *ContextBudget) []PromptFragment`
- Added `contextBudget` field to `Runtime` + `SetContextBudget` + `GetBudgetedPromptFragments` methods
