# Plan: GitHub & GitLab Platform Automation

Adds first-class support for GitHub and GitLab APIs/CLIs, enabling Rubichan to
post PR comments, manage issues, annotate code, and integrate into CI/CD
pipelines on both platforms. Informed by Claude Code's GitHub automation patterns
(event-driven workflows, label-based state, AI-powered triage) and the Rubichan
spec (FR-2.4, FR-2.8, FR-4.10).

## Reference: Claude Code Patterns Worth Adopting

From DeepWiki analysis of `anthropics/claude-code`:
- **Event-driven architecture** — workflows triggered by repo events, not polling
- **Label-based state management** — issue/PR lifecycle tracked via labels
- **Separation of concerns** — AI handles judgment (triage, review), scripts handle deterministic state transitions
- **Human-in-the-loop safeguards** — grace periods, reaction monitoring, activity detection
- **Structured output** — SARIF for security, PR comments for reviews

## Architecture

```
internal/platform/           # NEW — git hosting platform abstraction
├── platform.go              # Platform interface + types (PR, Issue, Comment, ReviewComment)
├── github.go                # GitHub implementation (go-github + gh CLI fallback)
├── gitlab.go                # GitLab implementation (go-gitlab + glab CLI fallback)
├── detect.go                # Auto-detect platform from git remote URL
├── github_test.go
├── gitlab_test.go
├── detect_test.go
└── platform_test.go

internal/output/
├── sarif.go                 # NEW — SARIF v2.1.0 formatter (owenrumney/go-sarif)
├── sarif_test.go
├── pr_comment.go            # NEW — PR comment formatter (Markdown with annotations)
├── pr_comment_test.go
├── github_annotations.go    # NEW — GitHub Actions workflow annotations (::error, ::warning)
└── github_annotations_test.go
```

---

## Feature 1: Platform Abstraction Layer (`internal/platform/`)

**Goal**: Define a unified interface for interacting with GitHub and GitLab,
enabling all downstream features (PR comments, issue management, CI annotations)
to be platform-agnostic.

### Interface Design

```go
// Platform abstracts git hosting platform operations.
type Platform interface {
    // Identity
    Name() string  // "github" or "gitlab"

    // Pull/Merge Requests
    PostPRComment(ctx context.Context, repo string, prNumber int, body string) error
    PostPRReviewComment(ctx context.Context, repo string, prNumber int, comment ReviewComment) error
    PostPRReview(ctx context.Context, repo string, prNumber int, review Review) error
    GetPRDiff(ctx context.Context, repo string, prNumber int) (string, error)
    ListPRFiles(ctx context.Context, repo string, prNumber int) ([]PRFile, error)

    // Issues
    CreateIssue(ctx context.Context, repo string, issue Issue) (*Issue, error)
    AddLabels(ctx context.Context, repo string, issueNumber int, labels []string) error
    AddComment(ctx context.Context, repo string, issueNumber int, body string) error

    // CI Integration
    GetCheckRunStatus(ctx context.Context, repo string, ref string) ([]CheckRun, error)
    CreateCheckRun(ctx context.Context, repo string, check CheckRun) error
    UpdateCheckRun(ctx context.Context, repo string, checkID int64, check CheckRun) error
}
```

### Types

```go
type ReviewComment struct {
    Path     string
    Line     int
    Body     string
    Side     string  // "LEFT" or "RIGHT"
}

type Review struct {
    Body     string
    Event    string  // "APPROVE", "REQUEST_CHANGES", "COMMENT"
    Comments []ReviewComment
}

type PRFile struct {
    Filename string
    Status   string  // "added", "modified", "removed"
    Patch    string
}

type Issue struct {
    Number int
    Title  string
    Body   string
    Labels []string
    State  string
}

type CheckRun struct {
    ID         int64
    Name       string
    Status     string  // "queued", "in_progress", "completed"
    Conclusion string  // "success", "failure", "neutral", "action_required"
    Output     CheckRunOutput
}

type CheckRunOutput struct {
    Title       string
    Summary     string
    Annotations []Annotation
}

type Annotation struct {
    Path      string
    StartLine int
    EndLine   int
    Level     string  // "notice", "warning", "failure"
    Message   string
    Title     string
}
```

### Tests

- [ ] **1.1** `TestPlatformInterfaceCompliance_GitHub` — GitHub client implements Platform interface
- [ ] **1.2** `TestPlatformInterfaceCompliance_GitLab` — GitLab client implements Platform interface
- [ ] **1.3** `TestDetectPlatformFromRemote_GitHub` — `git@github.com:owner/repo.git` → GitHub
- [ ] **1.4** `TestDetectPlatformFromRemote_GitLabSSH` — `git@gitlab.com:group/repo.git` → GitLab
- [ ] **1.5** `TestDetectPlatformFromRemote_GitLabHTTPS` — `https://gitlab.example.com/group/repo` → GitLab
- [ ] **1.6** `TestDetectPlatformFromRemote_Unknown` — Unknown host → error
- [ ] **1.7** `TestDetectPlatformFromEnv` — `GITHUB_ACTIONS=true` → GitHub; `GITLAB_CI=true` → GitLab
- [ ] **1.8** `TestParseRepoFromRemote` — Extracts `owner/repo` from SSH and HTTPS remote URLs
- [ ] **1.9** `TestGitHubPostPRComment` — Posts comment via go-github (HTTP mock)
- [ ] **1.10** `TestGitHubPostPRReviewComment` — Posts inline review comment (HTTP mock)
- [ ] **1.11** `TestGitHubPostPRReview` — Submits full review with inline comments (HTTP mock)
- [ ] **1.12** `TestGitHubCreateCheckRun` — Creates check run with annotations (HTTP mock)
- [ ] **1.13** `TestGitLabPostMRComment` — Posts MR note via go-gitlab (HTTP mock)
- [ ] **1.14** `TestGitLabPostMRReviewComment` — Posts inline MR discussion (HTTP mock)
- [ ] **1.15** `TestGitLabCreateIssue` — Creates issue via go-gitlab (HTTP mock)
- [ ] **1.16** `TestGitHubCLIFallback` — When no token, falls back to `gh` CLI
- [ ] **1.17** `TestGitLabCLIFallback` — When no token, falls back to `glab` CLI

### Implementation Notes

- GitHub: use `google/go-github` (already in spec's tech stack)
- GitLab: use `xanzy/go-gitlab` (MIT license, well-maintained)
- Auth: `GITHUB_TOKEN` / `GITLAB_TOKEN` env vars, with CLI fallback (`gh` / `glab`)
- Auto-detection order: env vars (`GITHUB_ACTIONS`, `GITLAB_CI`) → git remote URL parsing

---

## Feature 2: SARIF Output Formatter (`internal/output/sarif.go`)

**Goal**: Produce SARIF v2.1.0 output from security findings, enabling native
integration with GitHub Code Scanning, GitLab SAST reports, and VS Code.

### Tests

- [ ] **2.1** `TestSARIFFormatterEmpty` — Empty findings → valid SARIF with empty results array
- [ ] **2.2** `TestSARIFFormatterSingleFinding` — One finding maps to correct SARIF result (ruleId, level, location)
- [ ] **2.3** `TestSARIFFormatterSeverityMapping` — critical→error, high→error, medium→warning, low→note, info→note
- [ ] **2.4** `TestSARIFFormatterMultipleFindings` — Multiple findings in correct order with distinct rule entries
- [ ] **2.5** `TestSARIFFormatterWithLocation` — File + line → SARIF physicalLocation with artifactLocation + region
- [ ] **2.6** `TestSARIFFormatterNoLocation` — Finding without file/line → result without location (still valid)
- [ ] **2.7** `TestSARIFFormatterValidatesAgainstSchema` — Output validates against SARIF v2.1.0 JSON schema
- [ ] **2.8** `TestSARIFFormatterToolInfo` — Includes rubichan tool info (name, version, semanticVersion)

### Implementation Notes

- Use `owenrumney/go-sarif` library (already in spec's tech stack)
- Map `SecurityFinding.Severity` to SARIF levels
- Include tool component info for each scanner
- Upload via `gh` CLI: `gh api -X POST repos/{owner}/{repo}/code-scanning/sarifs`

---

## Feature 3: PR Comment Formatter (`internal/output/pr_comment.go`)

**Goal**: Format RunResult as a well-structured PR comment suitable for posting
via the Platform interface.

### Tests

- [ ] **3.1** `TestPRCommentFormatterBasic` — Produces Markdown with summary header, response body
- [ ] **3.2** `TestPRCommentFormatterWithFindings` — Security findings render as severity-grouped table
- [ ] **3.3** `TestPRCommentFormatterWithToolCalls` — Tool calls shown in collapsible details section
- [ ] **3.4** `TestPRCommentFormatterCollapsesLongOutput` — Response > 65000 chars truncated with "see full report" link
- [ ] **3.5** `TestPRCommentFormatterCodeReviewMode` — Code review mode adds inline suggestion blocks
- [ ] **3.6** `TestPRCommentFormatterErrorResult` — Error result shows error message prominently
- [ ] **3.7** `TestPRCommentFormatterSecuritySummaryBadge` — Summary includes severity counts as badges/emoji

### Implementation Notes

- Max GitHub comment size is ~65536 chars; truncate with link to full report
- Use collapsible `<details>` blocks for tool calls and evidence
- Security findings grouped by severity with emoji indicators
- Code review suggestions use GitHub's suggestion block syntax:
  ````
  ```suggestion
  corrected code here
  ```
  ````

---

## Feature 4: GitHub Actions Annotations (`internal/output/github_annotations.go`)

**Goal**: Emit `::error`, `::warning`, `::notice` workflow commands so findings
appear as inline annotations in GitHub's PR file view.

### Tests

- [ ] **4.1** `TestGitHubAnnotationsSingleFinding` — One finding → `::warning file=...,line=...::message`
- [ ] **4.2** `TestGitHubAnnotationsSeverityMapping` — critical/high→error, medium→warning, low/info→notice
- [ ] **4.3** `TestGitHubAnnotationsMultipleFindings` — Multiple findings each on their own line
- [ ] **4.4** `TestGitHubAnnotationsEscaping` — Special chars in messages properly escaped (`%0A`, `%25`)
- [ ] **4.5** `TestGitHubAnnotationsNoFile` — Finding without file → annotation without file/line params

### Implementation Notes

- Only emitted when `GITHUB_ACTIONS=true` env var is set
- Format: `::warning file={path},line={line},endLine={endLine},title={title}::{message}`
- Escape `%`, `\n`, `\r` per GitHub's workflow command spec

---

## Feature 5: CI Pipeline Integration (`--post-to-pr` flag)

**Goal**: Add end-to-end CI integration so Rubichan can auto-detect the platform,
run analysis, and post results back to the PR — all in one command.

### CLI Additions

```bash
# Auto-detect platform + PR, post comment
rubichan --headless --mode=code-review --post-to-pr

# Explicit PR number
rubichan --headless --mode=code-review --post-to-pr --pr=42

# SARIF upload to GitHub Code Scanning
rubichan --headless --mode=code-review --output=sarif --upload-sarif

# GitLab: post to MR + emit SAST report artifact
rubichan --headless --mode=code-review --post-to-pr --output=sarif > gl-sast-report.json
```

### Tests

- [ ] **5.1** `TestPostToPRFlag` — `--post-to-pr` flag is registered and parsed
- [ ] **5.2** `TestPostToPRAutoDetectsPlatform` — Detects GitHub/GitLab from environment
- [ ] **5.3** `TestPostToPRAutoDetectsPRNumber` — Reads PR number from `GITHUB_REF` or `CI_MERGE_REQUEST_IID`
- [ ] **5.4** `TestPostToPRFormatsAndPosts` — Runs analysis, formats as PR comment, posts via Platform
- [ ] **5.5** `TestUploadSARIF` — `--upload-sarif` uploads SARIF to GitHub Code Scanning API
- [ ] **5.6** `TestPostToPRExplicitNumber` — `--pr=42` overrides auto-detection
- [ ] **5.7** `TestPostToPRNoPlatformDetected` — Error message when platform cannot be determined

### Implementation Notes

- PR number auto-detection:
  - GitHub Actions: parse `GITHUB_REF` (`refs/pull/42/merge`) or `GITHUB_EVENT_PATH`
  - GitLab CI: read `CI_MERGE_REQUEST_IID` env var
- Repo auto-detection: parse `GITHUB_REPOSITORY` or `CI_PROJECT_PATH`

---

## Feature 6: Example CI Workflow Files (FR-2.8)

**Goal**: Provide ready-to-use CI configuration files that users can drop into
their repos.

### Files

- [ ] **6.1** `examples/ci/github-actions.yml` — GitHub Actions workflow for PR code review
- [ ] **6.2** `examples/ci/gitlab-ci.yml` — GitLab CI pipeline with SAST artifact
- [ ] **6.3** `examples/ci/jenkinsfile` — Jenkins pipeline with SARIF archiving

### GitHub Actions Example

```yaml
name: Rubichan Code Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
      security-events: write  # for SARIF upload
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Install Rubichan
        run: go install github.com/julianshen/rubichan/cmd/rubichan@latest
      - name: Run Code Review
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: rubichan --headless --mode=code-review --post-to-pr
      - name: Upload SARIF
        if: always()
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          rubichan --headless --mode=code-review --output=sarif > results.sarif
          gh api -X POST repos/${{ github.repository }}/code-scanning/sarifs \
            --field sarif=@results.sarif --field ref=${{ github.ref }}
```

### GitLab CI Example

```yaml
rubichan-review:
  stage: test
  image: golang:1.22
  script:
    - go install github.com/julianshen/rubichan/cmd/rubichan@latest
    - rubichan --headless --mode=code-review --post-to-pr --output=sarif > gl-sast-report.json
  artifacts:
    reports:
      sast: gl-sast-report.json
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  variables:
    ANTHROPIC_API_KEY: $ANTHROPIC_API_KEY
    GITLAB_TOKEN: $CI_JOB_TOKEN
```

---

## Dependencies

| Library | Purpose | License |
|---------|---------|---------|
| `google/go-github/v60` | GitHub API client | BSD-3 (already in spec) |
| `xanzy/go-gitlab` | GitLab API client | Apache-2.0 |
| `owenrumney/go-sarif/v2` | SARIF generation | MIT (already in spec) |

---

## Implementation Order

1. **Feature 1** (Platform Abstraction) — foundation for everything else
2. **Feature 2** (SARIF Formatter) — standalone, no platform dependency
3. **Feature 3** (PR Comment Formatter) — standalone, no platform dependency
4. **Feature 4** (GitHub Annotations) — standalone, GitHub-specific
5. **Feature 5** (CI Integration) — ties Features 1-4 together
6. **Feature 6** (Example Configs) — documentation, done last

Features 2, 3, and 4 can be developed in parallel since they're independent formatters.

---

## Spec Alignment

| Spec Requirement | Feature | Status |
|-----------------|---------|--------|
| FR-2.4: SARIF output | Feature 2 | Planned |
| FR-2.4: GitHub PR comment | Features 3 + 5 | Planned |
| FR-2.8: GitHub Actions integration | Features 5 + 6.1 | Planned |
| FR-2.8: GitLab CI integration | Features 5 + 6.2 | Planned |
| FR-2.8: Jenkins integration | Feature 6.3 | Planned |
| FR-4.10: SARIF security output | Feature 2 | Planned |
| FR-4.10: GitHub PR annotations | Feature 4 | Planned |
