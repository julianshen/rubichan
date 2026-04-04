# Skill Install Sources & Discovery Enhancement — Design Spec

## Goal

Extend rubichan's skill system with git/npm/GitHub install sources, manifest category and tags, enhanced discovery commands, and a skill update mechanism — closing the gap with Claude Code's plugin marketplace without introducing a separate plugin format.

## Architecture Overview

The existing `SKILL.yaml` manifest already supports multi-component bundles (tools, agents, hooks, commands). This enhancement focuses on three areas: (1) how skills are installed (new source types), (2) how skills are described (category + tags), and (3) how skills are discovered and maintained (search filters, update command).

No new manifest format is introduced. `SKILL.yaml` remains the single package unit.

## 1. New Install Sources

### Source Resolution

```bash
# Local path (existing, unchanged)
rubichan skill install ./my-skill
rubichan skill install /absolute/path/to/skill

# Git repository
rubichan skill install git:https://github.com/user/my-skill.git
rubichan skill install git:https://github.com/user/my-skill.git@v1.2.0

# GitHub shorthand
rubichan skill install github:user/my-skill
rubichan skill install github:user/my-skill@v1.2.0

# npm package
rubichan skill install npm:rubichan-skill-code-review
rubichan skill install npm:rubichan-skill-code-review@1.0.0

# Registry name (existing, unchanged)
rubichan skill install code-reviewer
rubichan skill install code-reviewer@1.0.0
```

### Resolution Logic

1. Starts with `./`, `/`, or is a path containing path separators → **local path**
2. Starts with `git:` → **git clone**
3. Starts with `github:` → expand to `git:https://github.com/{user}/{repo}.git`, then git clone
4. Starts with `npm:` → **npm pack + extract**
5. No prefix, no path separators → **registry lookup** (existing behavior)

### Git Install Flow

1. Parse URL and optional `@ref` (tag, branch, or commit)
2. `git clone --depth 1 [--branch <ref>]` to a temporary directory
3. Validate `SKILL.yaml` exists at the repo root
4. Run `skill test` validation on the cloned directory
5. Copy skill contents to `~/.config/rubichan/skills/<name>/`
6. Record source metadata in skills database:
   - `source_type`: `"git"`
   - `source_url`: the full git URL
   - `source_ref`: the tag/branch/commit (or `"HEAD"` if unspecified)
7. Clean up temporary directory

Version pinning: When `@ref` is provided, `--branch <ref>` is passed to git clone. When absent, the default branch is cloned and `source_ref` is recorded as the cloned commit hash.

### GitHub Shorthand

`github:user/repo` is syntactic sugar. Expansion:
- `github:user/repo` → `git:https://github.com/user/repo.git`
- `github:user/repo@v1.0` → `git:https://github.com/user/repo.git@v1.0`

The expanded URL is used for all git operations. The `source_type` is recorded as `"github"` (distinct from generic `"git"`) so `skill list` can display the short form.

### npm Install Flow

1. Parse package name and optional `@version`
2. Run `npm pack <package>[@<version>]` in a temporary directory to download the tarball
3. Extract the tarball (`tar xzf`) — npm packs into a `package/` subdirectory
4. Validate `SKILL.yaml` exists in the extracted `package/` directory
5. Run `skill test` validation
6. Copy contents to `~/.config/rubichan/skills/<name>/`
7. Record source metadata:
   - `source_type`: `"npm"`
   - `source_package`: the npm package name
   - `source_version`: the installed version
8. Clean up temporary files

npm must be available on PATH. If not found, the command fails with a clear error message: `"npm not found — install Node.js to use npm: sources"`.

### Error Handling

- Git clone failure → clear error with URL and git stderr
- npm pack failure → clear error with package name and npm stderr
- Missing `SKILL.yaml` → `"no SKILL.yaml found in <source> — is this a rubichan skill?"`
- Validation failure → same error as `skill test`
- Name collision → `"skill <name> is already installed — use skill remove first or skill update"`

## 2. Manifest Extensions

### New Fields

```yaml
name: code-reviewer
version: 1.0.0
description: Multi-agent code review with style checking
category: development              # NEW — enum, optional
tags: [code-review, golang, ci]    # NEW — free-form strings, optional

types:
  - prompt
  - tool
permissions:
  - file:read
# ... rest of manifest unchanged
```

### Category Enum

| Category | Description |
|----------|-------------|
| `development` | Coding tools, code generation, refactoring, debugging |
| `productivity` | Workflow automation, git helpers, documentation, project management |
| `learning` | Educational tools, explanations, tutorials, onboarding |
| `security` | Scanning, auditing, compliance, vulnerability detection |
| `testing` | Test generation, coverage analysis, validation, benchmarking |

### Tags

- Free-form lowercase strings
- No validation beyond non-empty
- Used for `skill search` filtering
- Convention: use hyphens (`code-review`), not underscores or spaces

### Backward Compatibility

Both fields are optional. Existing skills without `category` or `tags` continue to work without modification.

- `skill test` emits a warning (not error) when `category` is missing: `"warning: no category specified — consider adding one for discoverability"`
- `skill list` shows a `CATEGORY` column; skills without one show `-`
- `skill search` matches against name, description, category, and tags

### Validation

- `category` must be one of the five enum values (case-insensitive)
- `tags` must be an array of non-empty strings
- Invalid category in `skill test` → error (if present, must be valid)

## 3. Enhanced Discovery Commands

### `skill list` — Category Column

```
rubichan skill list

NAME            VERSION  CATEGORY      SOURCE                              INSTALLED
code-reviewer   1.0.0    development   github:user/code-reviewer@v1.0.0   2026-03-29
doc-generator   0.2.0    productivity  ./local-skills/doc-gen              2026-03-28
sec-scanner     2.1.0    security      npm:rubichan-sec-scanner@2.1.0     2026-03-27
my-helper       0.1.0    -             /tmp/test-skill                    2026-03-29
```

Source column displays the install source in its canonical short form:
- Local: the path used at install time
- Git: `git:<url>@<ref>`
- GitHub: `github:user/repo@<ref>`
- npm: `npm:<package>@<version>`
- Registry: `registry:<name>@<version>`

### `skill search` — Filters

```bash
# Keyword search (searches name, description, category, tags)
rubichan skill search "code review"

# Filter by category
rubichan skill search --category development

# Filter by tag
rubichan skill search --tag golang

# Combined
rubichan skill search --category security --tag compliance "audit"
```

Search checks both locally installed skills and the remote registry (when reachable). Local results are marked `[installed]`.

### `skill info` — Component Summary

```
rubichan skill info code-reviewer

Name:        code-reviewer
Version:     1.0.0
Description: Multi-agent code review with style checking
Category:    development
Tags:        code-review, golang, testing, ci
Types:       prompt, tool
Source:      github:user/code-reviewer@v1.0.0
Installed:   2026-03-29T07:18:56Z
Components:  2 tools, 3 agents, 1 command, 4 hooks
Permissions: file:read, shell:exec
```

The `Components` line counts entries from `tools[]`, `agents[]`, `commands[]`, and hook registrations in the manifest. This makes multi-component skills (bundles) visible without a separate plugin concept.

### `skill update` — New Command

```bash
# Update a specific skill to latest
rubichan skill update code-reviewer

# Update all git/npm-installed skills
rubichan skill update --all

# Dry run — show what would change
rubichan skill update --all --dry-run
```

**Update behavior by source type:**

| Source | Update Action |
|--------|--------------|
| `git` / `github` | Re-clone default branch (or pinned ref), compare version in `SKILL.yaml`. If newer, reinstall. |
| `npm` | `npm view <package> version`, compare with installed. If newer, re-install. |
| `local` | No-op. Print: `"<name> was installed from a local path — reinstall manually"` |
| `registry` | Check registry for newer version. If available, download and reinstall. |

**Version comparison:** Semantic versioning (`semver`). If the remote version is greater than the installed version, the update proceeds. If equal, print `"<name> is already up to date (v<version>)"`.

**Output:**

```
rubichan skill update --all
Checking code-reviewer (github:user/code-reviewer)... 1.0.0 → 1.1.0 ✓
Checking doc-generator (local path)... skipped (local install)
Checking sec-scanner (npm:rubichan-sec-scanner)... up to date (2.1.0)
Updated 1 skill, 1 skipped, 1 up to date.
```

## 4. Skills Database Schema Extension

The existing skills database (SQLite at `~/.config/rubichan/skills.db`) needs new columns:

```sql
ALTER TABLE skills ADD COLUMN source_type TEXT DEFAULT 'local';
ALTER TABLE skills ADD COLUMN source_url TEXT DEFAULT '';
ALTER TABLE skills ADD COLUMN source_ref TEXT DEFAULT '';
ALTER TABLE skills ADD COLUMN category TEXT DEFAULT '';
ALTER TABLE skills ADD COLUMN tags TEXT DEFAULT '';  -- JSON array stored as text
```

Migration runs automatically on first access after upgrade. Existing rows get default values.

## 5. Implementation Files

| File | Responsibility |
|------|---------------|
| `internal/skills/installer.go` | New: source resolution, git/npm/github install flows |
| `internal/skills/installer_test.go` | Tests for each install source |
| `internal/skills/updater.go` | New: skill update logic |
| `internal/skills/updater_test.go` | Tests for update flows |
| `internal/skills/manifest.go` | Modify: add `Category` and `Tags` fields |
| `internal/skills/manifest_test.go` | Modify: validation tests for new fields |
| `internal/store/skills.go` | Modify: extend schema with new columns |
| `cmd/rubichan/main.go` | Modify: wire `skill update` subcommand, update install/search/list/info |

## 6. Error Handling

- All install sources are non-destructive: if validation fails after clone/extract, the temporary directory is cleaned up and the skills directory is untouched.
- Network failures (git clone, npm pack) produce clear errors with the command that failed and its stderr.
- `skill update` failures are per-skill: one failure doesn't stop updates to other skills.
- npm absence is detected early with a clear message.
- Git absence is detected early with a clear message.

## 7. Not In Scope (YAGNI)

- Git subdirectory support (monorepos use local path install)
- Marketplace backend/API server
- Skill ratings, reviews, or download counts
- Enterprise managed settings / allowlisting
- Dependency resolution across skills during install
- Automatic updates (always manual via `skill update`)
- Lock files for reproducible installs
