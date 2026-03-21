# Skill Authoring Patterns — Design Spec

**Date:** 2026-03-21
**Status:** Draft
**Scope:** Spec section 4.13, built-in skill improvements, budgeting enhancement note

## Problem

Rubichan's skill system supports prompt skills (SKILL.md / SKILL.yaml with system prompts injected into context), but the spec provides no guidance on *how to write effective prompt content*. Current built-in prompt skills range from structured and effective (frontend-design) to pure reference dumps (apple-dev). Studying Claude Code's official frontend-design plugin reveals five structural patterns that dramatically improve prompt skill effectiveness. These patterns should be codified in the spec and applied to existing built-in skills.

## Analysis: What Makes Effective Prompt Skills

Comparing three built-in prompt skills by structure:

| Skill | Lines | Thinking Phase | Anti-Patterns | Calibration | Verification |
|-------|-------|:-:|:-:|:-:|:-:|
| `frontend-design` | 68 | Yes | Yes | Yes | No |
| `ui-ux-pro-max` | 391 | Partial (step-based) | Yes (tables) | No | Yes (checklist) |
| `apple-dev` | 187 | No | No | No | No |

The frontend-design skill achieves high behavioral impact with only 68 lines because it structures *how the LLM thinks*, not just what it knows. The apple-dev skill has 3x the content but less behavioral impact because it's a flat knowledge dump.

Key insight: **prompt skill effectiveness comes from structure, not volume.** A 42-line skill with the right patterns outperforms a 400-line reference document.

## Design

### 1. New Spec Section: 4.13 Skill Authoring Patterns

Add after section 4.12 (Skill Registry & Distribution). This section defines five structural patterns for prompt skill content.

#### Pattern 1: Pre-Action Thinking Phase

Every prompt skill should open with a structured decision framework that forces the LLM to analyze context before acting. The thinking phase transforms a skill from "here are rules to follow" into "here's how to reason about this domain."

**Structure:**
```markdown
## Thinking Phase

Before acting, analyze:
- **Context**: What is the user's situation? What constraints exist?
- **Approach**: What strategy fits this specific case?
- **Calibration**: How much depth/complexity does this warrant?
- **Differentiation**: What makes this response specifically useful vs generic?
```

**Why:** LLMs default to pattern-matching from training data. A thinking phase forces situation-specific analysis, producing output tailored to the actual context rather than statistical averages.

**Example (from frontend-design):**
```markdown
Before coding, understand the context and commit to a BOLD aesthetic direction:
- Purpose: What problem does this interface solve? Who uses it?
- Tone: Pick an extreme (list of aesthetic directions)
- Constraints: Technical requirements
- Differentiation: What makes this UNFORGETTABLE?
```

#### Pattern 2: Anti-Pattern Lists

Explicitly name what the LLM must NOT do. LLMs have statistical favorites — outputs they gravitate toward because those patterns appear frequently in training data. Naming these defaults breaks mode-collapse and forces genuine reasoning.

**Structure:**
```markdown
## Anti-Patterns

NEVER:
- [specific bad default the LLM gravitates toward]
- [named cliché that seems helpful but produces generic output]
- [common shortcut that sacrifices quality]
```

**Why:** "Do X well" is vague. "Never do Y" is precise and falsifiable. Anti-pattern lists create hard boundaries that prevent the most common failure modes.

**Example (from frontend-design):**
```markdown
NEVER use generic AI-generated aesthetics like overused font families
(Inter, Roboto, Arial), cliched color schemes (purple gradients on white),
predictable layouts and component patterns.
```

**Guideline:** Anti-patterns should name *specific* defaults, not abstract categories. "Don't use bad fonts" is useless. "Don't use Inter, Roboto, Arial, or Space Grotesk" is actionable.

#### Pattern 3: Complexity-Vision Matching (Calibration)

Calibrate output depth to the task's intent, not to a fixed level. A prompt skill that always produces maximum-detail output wastes tokens on simple tasks and under-serves complex ones.

**Structure:**
```markdown
## Calibration

Match implementation depth to the task:
- Simple/routine tasks → precise, minimal output
- Complex/novel tasks → elaborate, detailed output
- The appropriate level of detail depends on [domain-specific signal]
```

**Why:** Without calibration, LLMs default to a single output depth (usually medium-verbose). Calibration prevents both over-engineering simple requests and under-serving complex ones.

**Example (from frontend-design):**
```markdown
Match implementation complexity to the aesthetic vision. Maximalist designs
need elaborate code with extensive animations and effects. Minimalist or
refined designs need restraint, precision, and careful attention to spacing,
typography, and subtle details.
```

#### Pattern 4: Inspirational Anchoring

Provide creative seeds — a set of named approaches, directions, or extremes — that give the LLM a starting vocabulary without prescribing a template. These anchors prevent the blank-page problem while preserving creative latitude.

**Structure:**
```markdown
## Approaches

Consider directions like: [list of named extremes/options]

Use these for inspiration, not as templates. Choose the approach
that best fits the specific context.
```

**Why:** Without anchors, LLMs converge on the same 2-3 default approaches. A curated list of diverse options expands the solution space while keeping output grounded in recognized patterns.

**Example (from frontend-design):**
```markdown
Pick an extreme: brutally minimal, maximalist chaos, retro-futuristic,
organic/natural, luxury/refined, playful/toy-like, editorial/magazine,
brutalist/raw, art deco/geometric, soft/pastel, industrial/utilitarian
```

**Anti-convergence rule:** If a skill provides anchors, it should also instruct: "NEVER converge on the same choice across sessions." This prevents the LLM from developing a favorite.

#### Pattern 5: Pre-Delivery Verification

A checklist the LLM evaluates before presenting output. This catches common quality issues that slip through when the LLM is focused on content generation.

**Structure:**
```markdown
## Verification

Before delivering output:
- [ ] Output matches the chosen approach from the thinking phase
- [ ] Anti-patterns are avoided
- [ ] Depth is appropriate for the context
- [ ] [Domain-specific quality checks]
```

**Why:** Generation and verification are different cognitive modes. A verification section activates the LLM's critical evaluation after generation is complete. Without it, the LLM's self-assessment is ad-hoc and often skipped.

**Example (from ui-ux-pro-max):**
```markdown
Before delivering UI code, verify:
- [ ] No emojis used as icons (use SVG instead)
- [ ] All clickable elements have cursor-pointer
- [ ] Light mode text has sufficient contrast (4.5:1 minimum)
- [ ] Responsive at 375px, 768px, 1024px, 1440px
```

### Pattern Interaction

The five patterns form a pipeline:

```
Thinking → Anchoring → Calibration → [Generation] → Verification
   ↑                                                      ↑
   └────── Anti-Patterns constrain all stages ────────────┘
```

Not every prompt skill needs all five patterns. The minimum effective set depends on the skill type:

| Skill Domain | Required Patterns | Optional Patterns |
|-------------|-------------------|-------------------|
| Creative (design, writing) | Thinking, Anti-Patterns, Anchoring | Calibration, Verification |
| Reference (APIs, platforms) | Thinking, Anti-Patterns | Calibration |
| Operational (review, audit) | Thinking, Anti-Patterns, Verification | Calibration, Anchoring |
| Security (rules, scanning) | Thinking, Anti-Patterns, Verification | Calibration, Anchoring |

### 2. Built-in Skill Improvements

Apply the patterns to two existing built-in skills as concrete examples.

#### 2a: `code-review` prompt component

The spec (section 4.10) already defines `code-review` as a built-in `Workflow + Prompt` skill. This design adds the prompt component content following all five patterns. The existing superpowers built-in skills (`requesting-code-review`, `receiving-code-review`) provide workflow-level orchestration; this prompt component provides the domain knowledge that guides review quality.

Create `internal/skills/builtin/codereview/` using the same `embed.FS` + `frontmatter.RegisterAllFull` pattern as the `frontenddesign` skill. The SKILL.md uses YAML frontmatter parsed by `ParseInstructionSkill`, which supports `triggers.keywords` and `triggers.modes` fields.

```markdown
---
name: review-guide
version: 1.0.0
description: Structured code review guidance for pull requests and diffs
triggers:
  keywords:
    - review
    - PR
    - pull request
    - diff
  modes:
    - interactive
    - headless
---

Guide for conducting effective, actionable code reviews.

## Thinking Phase

Before reviewing, analyze:
- **Scope**: Is this a bug fix, feature, refactor, or config change?
- **Risk**: What's the blast radius? (data model change > UI tweak)
- **Conventions**: What patterns does this codebase already use?
- **Focus**: What matters most for THIS specific change?

## Approaches

Prioritize review focus based on the change type:
- **Correctness-first**: Bug fixes, data handling, state management
- **Security-first**: Auth changes, input handling, API endpoints
- **Architecture-first**: New abstractions, interface changes, dependency additions
- **Performance-first**: Hot paths, database queries, serialization

Choose the lens that fits the change. Most reviews need only one primary focus.

## Anti-Patterns

NEVER:
- Nitpick formatting that a linter should catch (indentation, trailing whitespace)
- Suggest rewriting working code that isn't part of the change
- Flag issues without providing a concrete fix or alternative
- Treat all issues as equal severity — distinguish critical from minor
- Bikeshed naming unless the name is actively misleading
- Request changes for personal style preferences

## Calibration

Match review depth to change risk:
- **Trivial** (typo fix, comment update) → Approve with minimal comment
- **Routine** (standard feature, clear tests) → Focus on edge cases and test coverage
- **Significant** (new abstraction, API change) → Deep review of design, error handling, backwards compatibility
- **Critical** (security, data migration) → Line-by-line review, request additional reviewers

## Verification

Before delivering review:
- [ ] Every issue includes severity (critical/major/minor/nit)
- [ ] Critical issues have concrete fix suggestions
- [ ] Positive feedback included for good patterns found
- [ ] Review addresses the change's actual purpose, not hypothetical improvements
```

#### 2b: `apple-dev` system prompt restructure

Restructure the existing `system.md` by adding a thinking phase and anti-patterns section at the top, before the reference material. The reference content stays but is now preceded by behavioral guidance.

**Registration mechanism note:** The apple-dev skill uses `RegisterPrompt()` in `prompt.go`, which directly constructs a `SkillManifest` and sets `Prompt.SystemPromptFile` to the embedded string content. This is different from the SKILL.md frontmatter pipeline used by `frontenddesign` and `superpowers`. The restructure only modifies the content of `system.md`; the registration mechanism remains unchanged.

```markdown
# Apple Platform Development Expert

## Thinking Phase

Before assisting with Apple platform code, assess:
- **Platform**: iOS, macOS, watchOS, tvOS, or visionOS?
- **Lifecycle stage**: Prototyping (skip signing/manifests) or shipping (full checklist)?
- **Minimum deployment target**: Determines available APIs and required availability checks
- **Architecture**: SwiftUI-first, UIKit legacy, or hybrid?

## Anti-Patterns

NEVER:
- Suggest NavigationView (deprecated — use NavigationStack or NavigationSplitView)
- Use @StateObject/@ObservedObject/@EnvironmentObject when targeting iOS 17+ (use @Observable)
- Recommend DispatchQueue/NSLock for new code (use actors and structured concurrency)
- Ignore strict concurrency warnings (enable SWIFT_STRICT_CONCURRENCY=complete)
- Use force-unwrap (!) in production code paths
- Suggest Storyboards for new projects (use SwiftUI or programmatic UIKit)

## Calibration

Match guidance depth to the development stage:
- **Exploring/prototyping** → Focus on API usage and SwiftUI patterns; skip signing, CI, and privacy manifests
- **Building features** → Include testing patterns, concurrency best practices, and state management
- **Preparing for release** → Full checklist: signing, privacy manifests, entitlements, App Store submission

[existing reference content follows unchanged]
```

### 3. Section-Aware Prompt Budgeting (Future Enhancement)

Add a note to section 4.5 or as a subsection of 4.13:

> **Future enhancement — Section-priority budgeting:** The prompt budgeting system (PromptCollector.BudgetedFragments) currently treats prompt content as an opaque blob, truncating from the end when over budget. A future enhancement could recognize markdown `##` section headers and assign priority weights to named sections. Sections like `## Anti-Patterns` and `## Thinking Phase` would receive higher priority than `## Reference` sections, ensuring behavioral guidance survives truncation even when reference material is cut. This requires no manifest schema changes — section headers in the markdown body serve as implicit structure.
>
> Proposed priority order (highest to lowest):
> 1. `## Anti-Patterns` — prevents harmful defaults
> 2. `## Thinking Phase` — ensures context analysis
> 3. `## Calibration` — scales output appropriately
> 4. `## Verification` — quality gate
> 5. `## Approaches` — creative direction (the pattern is named "Inspirational Anchoring" but the recommended section header is `## Approaches`)
> 6. All other sections — reference material

## Implementation Plan

### Phase 1: Spec Update
1. Add section 4.13 "Skill Authoring Patterns" to `spec.md`
2. Add section-priority budgeting note to spec
3. Add a cross-reference in section 4.11 pointing to section 4.13 for authoring guidance

### Phase 2: Built-in Skill Improvements
4. Create `internal/skills/builtin/codereview/` with SKILL.md + embed.go (using `frontmatter.RegisterAllFull` pattern)
5. Restructure `internal/skills/builtin/appledev/system.md` with thinking phase + anti-patterns (content-only change; `RegisterPrompt()` mechanism unchanged)
6. Add tests for new/modified built-in skills:
   - `codereview/`: verify skill loads via `frontmatter.RegisterAllFull`, prompt content contains `## Thinking Phase` and `## Anti-Patterns` headers
   - `appledev/`: verify `system.md` still loads correctly, content contains the new section headers

### Phase 3: Validation
7. Verify all built-in prompt skills load correctly
8. Verify prompt budgeting handles new content sizes
9. Update CLAUDE.md if needed

## Out of Scope

- Runtime implementation of section-aware budgeting (future work)
- Changes to SKILL.md/SKILL.yaml parsing or manifest schema
- Changes to the PromptCollector or budgeting code
- Modifications to the frontend-design or ui-ux-pro-max skills (they already follow the patterns)

## Trade-offs

**Chosen:** Content patterns over schema changes. The patterns are embedded in the markdown body of prompt skills, not in new manifest fields. This means:
- (+) Zero breaking changes to existing skills
- (+) Third-party skills adopt patterns by following the guide, not updating schemas
- (+) Immediate improvement to built-in skill quality
- (-) Patterns are advisory, not enforced at load time
- (-) Section-aware budgeting needs future work to get runtime benefit

**Alternative rejected:** Adding `sections` as a first-class manifest concept. Would require manifest schema changes, parser updates, and migration of all existing skills. The benefit (runtime section awareness) doesn't justify the cost at this stage.
