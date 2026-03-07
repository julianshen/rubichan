# Skill System Enhancements — Execution Plan

This document breaks the design into shippable phases with testable acceptance criteria.

## Status

- [x] Phase 0: Correctness Fixes
- [x] Phase 1: Recursive and Configurable Discovery
- [x] Phase 2: Relevance-Based Activation
- [x] Phase 3: Budgeting by Relevance
- [x] Phase 4: Richer `SKILL.md` Frontmatter
- [x] Phase 5: Subagent Skill Semantics
- [x] Phase 6: Developer Experience

## Phase 0: Correctness Fixes

### Task 0.1: Unify Project Skill Path

**Goal:** Ensure project-local skill install/add uses the same directory the loader scans.

Tests:

- `TestSkillAddUsesProjectSkillDir` — `skill add` writes to `.rubichan/skills/<name>/`
- `TestLoaderDiscoversAddedProjectSkill` — adding a skill makes it discoverable in the same repo

Implementation:

- Update `cmd/rubichan/skill.go`
- Confirm `createSkillRuntime` project dir behavior remains consistent

### Task 0.2: Support `SKILL.md` in CLI

**Goal:** Make instruction-only skills first-class in `skill info`, `skill test`, and `skill install`.

Tests:

- `TestSkillTestInstructionSkill`
- `TestSkillInfoInstructionSkill`
- `TestSkillInstallInstructionSkill`
- `TestSkillCreateInstructionTemplate`

Implementation:

- Add shared helper to read either `SKILL.yaml` or `SKILL.md`
- Update info/test/install/create code paths

### Task 0.3: Surface Discovery Warnings

**Goal:** Stop dropping loader warnings.

Tests:

- `TestRuntimeDiscoverReturnsOptionalDependencyWarnings`
- `TestCLIStartupPrintsSkillWarnings`

Implementation:

- Thread warnings through runtime creation and CLI startup logging

## Phase 1: Recursive and Configurable Discovery

### Task 1.1: Configurable Skill Roots

**Goal:** Support multiple configured skill directories.

Tests:

- `TestLoaderDiscoversFromConfiguredDirs`
- `TestLoaderConfiguredDirPrecedence`

Implementation:

- Extend config with `skills.dirs`
- Update loader construction in `cmd/rubichan/main.go`

### Task 1.2: Recursive Discovery

**Goal:** Discover skills at any depth below a configured root.

Tests:

- `TestScanDirRecursiveFindsNestedSkillYAML`
- `TestScanDirRecursiveFindsNestedSkillMD`
- `TestScanDirRecursivePrefersSkillYAMLOverSkillMD`

Implementation:

- Replace one-level `os.ReadDir` scan with recursive walk
- Preserve deterministic ordering

### Task 1.3: Add External Skill Directory Registration

**Goal:** Allow users to register skill-pack directories without copying files.

Tests:

- `TestSkillAddDirPersistsConfig`
- `TestLoaderIncludesRegisteredExternalDir`

Implementation:

- Add `skill add-dir <path>`
- Persist in config

### Task 1.4: Discovery Provenance

**Goal:** Track why and where a skill was discovered.

Tests:

- `TestDiscoveredSkillIncludesProvenance`
- `TestSkillInfoShowsProvenance`

Implementation:

- Add provenance struct/fields
- Expose in CLI output

## Phase 2: Relevance-Based Activation

### Task 2.1: Introduce Activation Score

**Goal:** Replace binary trigger evaluation with score calculation.

Tests:

- `TestActivationScoreExplicitWins`
- `TestActivationScoreCurrentPathBeatsGenericFileMatch`
- `TestActivationScoreBelowThresholdSkipsSkill`

Implementation:

- Add `ActivationScore` type
- Add score calculation helpers for file, language, keyword, mode, explicit

### Task 2.2: Thresholded Activation

**Goal:** Activate/import only skills above threshold.

Tests:

- `TestEvaluateTriggersUsesThreshold`
- `TestRuntimeEvaluateAndActivateUsesHighestScoringSkills`

Implementation:

- Replace `shouldActivate` boolean usage with score-aware flow

### Task 2.3: Activation Reporting

**Goal:** Explain why a skill loaded or did not load.

Tests:

- `TestRuntimeGetActivationReport`
- `TestSkillWhyRendersScoreBreakdown`

Implementation:

- Add `GetActivationReport()`
- Add `skill why <name>`

## Phase 3: Budgeting by Relevance

### Task 3.1: Relevance-Aware Fragment Ordering

**Goal:** Budget prompt fragments by activation score, not only source priority.

Tests:

- `TestPromptCollectorBudgetedFragmentsUsesActivationScore`
- `TestPromptCollectorBudgetedFragmentsExplicitSourceStillWinsTies`

Implementation:

- Extend `PromptFragment` metadata
- Update budget sorter

### Task 3.2: Truncation Reporting

**Goal:** Expose which fragments were truncated or excluded.

Tests:

- `TestRuntimeActivationReportIncludesTruncation`
- `TestSkillTraceShowsBudgetDecisions`

Implementation:

- Thread truncation decisions into trace/report output

## Phase 4: Richer `SKILL.md` Frontmatter

### Task 4.1: Add Structured Optional Fields

**Goal:** Extend instruction-skill parser with additive fields.

Tests:

- `TestParseInstructionSkillPriority`
- `TestParseInstructionSkillToolPolicies`
- `TestParseInstructionSkillReferences`
- `TestParseInstructionSkillAgentDefinitions`
- `TestParseInstructionSkillRejectsUnknownFieldWhenLinting`

Implementation:

- Expand parser structs
- Keep backward compatibility

### Task 4.2: Declarative Commands and Agents from `SKILL.md`

**Goal:** Allow markdown skills to contribute commands and agent definitions.

Tests:

- `TestRuntimeActivateInstructionSkillRegistersCommands`
- `TestRuntimeActivateInstructionSkillRegistersAgents`

Implementation:

- Wire parsed definitions into existing registries

### Task 4.3: Skill Linting

**Goal:** Add author-facing validation beyond parse success.

Tests:

- `TestSkillLintDetectsMissingReferenceFile`
- `TestSkillLintDetectsOversizedBody`
- `TestSkillLintDetectsDuplicateCommandNames`

Implementation:

- Add `skill lint <path>`

## Phase 5: Subagent Skill Semantics

### Task 5.1: Define Inheritance Defaults

**Goal:** Child agents inherit active skill indexes, not blind full prompt fragments.

Tests:

- `TestSubagentInheritsActiveSkillIndexes`
- `TestSubagentDoesNotBlindlyImportAllSkillBodies`

Implementation:

- Extend subagent config or options with skill context

### Task 5.2: Agent Definition Controls

**Goal:** Allow agent definitions to override inherited skill behavior.

Tests:

- `TestAgentDefinitionDisableSkills`
- `TestAgentDefinitionExtraSkills`
- `TestAgentDefinitionDisableInheritance`

Implementation:

- Extend agent definition types and registration

## Phase 6: Developer Experience

### Task 6.1: Skill Trace Mode

**Goal:** Add a single command for inspecting discovery, activation, and budget decisions.

Tests:

- `TestSkillTraceShowsDiscoveryActivationAndBudget`

Implementation:

- Add `skill trace`

### Task 6.2: Skill Dev Watch Mode

**Goal:** Provide a local authoring loop for skill creation.

Tests:

- `TestSkillDevReloadsOnSkillMDChange`
- `TestSkillDevPrintsValidationErrors`

Implementation:

- Add `skill dev <path>`
- Use filesystem watcher

### Task 6.3: Better Scaffolding

**Goal:** Scaffold instruction skills and richer bundles.

Tests:

- `TestSkillCreateInstruction`
- `TestSkillCreateWithAgents`
- `TestSkillCreateWithCommands`

Implementation:

- Add `skill create --type=instruction`
- Add optional flags for commands/agents

## Recommended PR Sequence

1. Phase 0
2. Phase 1.1 + 1.2
3. Phase 1.3 + 1.4
4. Phase 2.1 + 2.2
5. Phase 2.3 + Phase 3
6. Phase 4
7. Phase 5
8. Phase 6

## Definition of Done

- All new behavior is covered by focused unit tests
- Existing `SKILL.yaml` and legacy `SKILL.md` skills remain compatible
- CLI commands work for both manifest styles
- Activation decisions are explainable
- Subagent skill handling is deterministic
