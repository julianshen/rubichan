---
name: review-guide
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
