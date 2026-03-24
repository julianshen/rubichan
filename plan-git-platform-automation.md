# Plan: GitHub & GitLab Platform Automation

Adds first-class support for GitHub and GitLab APIs, enabling Rubichan to
post PR comments, manage issues, annotate code, and integrate into CI/CD
pipelines on both platforms. Informed by Claude Code's GitHub automation patterns
(event-driven workflows, label-based state, AI-powered triage) and the Rubichan
spec (FR-2.4, FR-2.8, FR-4.10).

## Key Finding: Existing Formatters Already Implemented

The following formatters **already exist** and must NOT be duplicated:

| Formatter | Location | Operates on |
|-----------|----------|-------------|
| SARIF v2.1.0 | `internal/security/output/sarif.go` | `*security.Report` |
| GitHub PR Review | `internal/security/output/github_pr.go` | `*security.Report` → `PRReview`/`PRComment` |
| PR Comment (Markdown) | `internal/output/pr_comment.go` | `*output.RunResult` |
| GitHub Actions Annotations | `internal/output/github_annotations.go` | `*output.RunResult` |
| JSON, Markdown, StyledMarkdown | `internal/output/*.go` | `*output.RunResult` |

The work remaining is:
1. **Platform abstraction layer** — actual API clients to post results
2. **CI environment detection** — auto-detect platform, repo, PR number
3. **Bridge** — connect existing formatters to platform API calls
4. **CLI wiring** — new flags in `runHeadless()` to trigger posting

---

## Architecture

```
internal/platform/           # NEW — git hosting platform abstraction
├── platform.go              # Platform interface + types
├── github.go                # GitHub impl via google/go-github/v68
├── gitlab.go                # GitLab impl via xanzy/go-gitlab
├── cienv.go                 # CI environment detection (env vars, git remote)
├── bridge.go                # Connects existing formatters → Platform API calls
├── platform_test.go         # Interface compliance tests
├── github_test.go           # GitHub API tests (httptest mocks)
├── gitlab_test.go           # GitLab API tests (httptest mocks)
├── cienv_test.go            # CI env detection tests
└── bridge_test.go           # Bridge integration tests
```

## ADR: Use go-github and go-gitlab SDKs (not raw net/http)

**Decision**: Use `google/go-github/v68` and `xanzy/go-gitlab` as primary API clients, with CLI (`gh`/`glab`) fallback when tokens are unavailable.

**Rationale**:
- The spec explicitly lists `google/go-github` in its tech stack
- ADR-006's "no vendor SDKs" restriction applies to **LLM** provider SDKs only, not platform SDKs
- SDK approach provides type-safe API coverage, automatic pagination, built-in retry/rate-limiting
- CLI fallback ensures functionality when API tokens are not available (e.g., local dev)
- Both libraries are well-maintained and permissively licensed (BSD-3 and Apache-2.0)

**Rejected alternative**: Raw `net/http` — would require reimplementing pagination, auth, rate limiting, type marshaling that the SDKs already handle correctly.

---

## PR 1: Platform Abstraction Layer (`internal/platform/`)

Foundation for everything else. Defines interface + GitHub and GitLab implementations using Go SDK libraries.

### Interface Design

```go
// Platform abstracts git hosting platform operations.
type Platform interface {
    Name() string // "github" or "gitlab"

    // Pull/Merge Requests
    PostPRComment(ctx context.Context, repo string, prNumber int, body string) error
    PostPRReview(ctx context.Context, repo string, prNumber int, review Review) error

    // CI Integration
    CreateCommitStatus(ctx context.Context, repo string, sha string, status CommitStatus) error
    UploadSARIF(ctx context.Context, repo string, ref string, sarifData []byte) error
}
```

The interface is deliberately small — only the operations needed for CI integration. Issue management and check runs can be added later via interface extension.

### Types

```go
type Review struct {
    Body     string
    Event    string // "APPROVE", "REQUEST_CHANGES", "COMMENT"
    Comments []ReviewComment
}

type ReviewComment struct {
    Path     string
    Line     int
    Body     string
    Side     string // "LEFT" or "RIGHT"
}

type CommitStatus struct {
    State       string // "success", "failure", "pending"
    Description string
    Context     string
    TargetURL   string
}
```

### Tests

- [ ] **1.01** `TestPlatformInterfaceCompliance_GitHub` — `*GitHubClient` satisfies `Platform`
- [ ] **1.02** `TestPlatformInterfaceCompliance_GitLab` — `*GitLabClient` satisfies `Platform`
- [ ] **1.03** `TestGitHubPostPRComment` — Posts comment via `go-github` Issues API (httptest mock)
- [ ] **1.04** `TestGitHubPostPRReview` — Creates review with inline comments via `go-github` (httptest mock)
- [ ] **1.05** `TestGitHubUploadSARIF` — Uploads gzip+base64 SARIF to code-scanning API (httptest mock)
- [ ] **1.06** `TestGitHubCreateCommitStatus` — Creates commit status via `go-github` (httptest mock)
- [ ] **1.07** `TestGitLabPostMRComment` — Posts MR note via `go-gitlab` (httptest mock)
- [ ] **1.08** `TestGitLabPostMRReview` — Posts MR discussions with position info via `go-gitlab` (httptest mock)
- [ ] **1.09** `TestGitLabUploadSARIF_NoOp` — GitLab SARIF upload is no-op (GitLab ingests via CI artifact)
- [ ] **1.10** `TestGitHubAuthFromToken` — `GITHUB_TOKEN` env var creates authenticated `go-github` client
- [ ] **1.11** `TestGitLabAuthFromToken` — `GITLAB_TOKEN` env var creates authenticated `go-gitlab` client
- [ ] **1.12** `TestGitHubNewClient_CustomBaseURL` — GitHub Enterprise URL configures `go-github` base URL
- [ ] **1.13** `TestGitLabNewClient_CustomBaseURL` — Self-hosted GitLab URL configures `go-gitlab` base URL

### Implementation Notes

- **GitHub**: Wrap `google/go-github/v68`. `PostPRReview` maps `Review.Comments` to `github.DraftReviewComment` entries. `UploadSARIF` uses `POST /repos/{owner}/{repo}/code-scanning/sarifs` with gzip+base64 encoding as GitHub requires.
- **GitLab**: Wrap `xanzy/go-gitlab`. For inline comments, use MR Discussions API with `PositionOptions`. `UploadSARIF` is a no-op — GitLab ingests SARIF as a CI artifact, not via API.
- **Auth**: Constructor accepts token directly; caller reads from env. No hidden env var coupling in the client.

---

## PR 2: CI Environment Detection (`internal/platform/cienv.go`)

Focused helper for detecting CI environment details — platform, repo, PR number, SHA.

### Struct Design

```go
type CIEnvironment struct {
    Provider string // "github-actions", "gitlab-ci", "jenkins"
    Repo     string // "owner/repo" or "group/project"
    PRNumber int    // 0 if not a PR context
    Ref      string // git ref
    SHA      string // commit SHA
    Token    string // auth token from env
}
```

### Tests

- [ ] **2.01** `TestParseRepoFromRemote_GitHubSSH` — `git@github.com:owner/repo.git` → `"owner/repo"`
- [ ] **2.02** `TestParseRepoFromRemote_GitHubHTTPS` — `https://github.com/owner/repo.git` → `"owner/repo"`
- [ ] **2.03** `TestParseRepoFromRemote_GitLabSSH` — `git@gitlab.com:group/project.git` → `"group/project"`
- [ ] **2.04** `TestParseRepoFromRemote_GitLabHTTPS` — `https://gitlab.example.com/group/sub/project` → `"group/sub/project"`
- [ ] **2.05** `TestParseRepoFromRemote_Invalid` — Invalid URL returns error
- [ ] **2.06** `TestDetectPlatformFromEnv_GitHub` — `GITHUB_ACTIONS=true` → GitHub
- [ ] **2.07** `TestDetectPlatformFromEnv_GitLab` — `GITLAB_CI=true` → GitLab
- [ ] **2.08** `TestDetectPlatformFromRemote_GitHub` — URL with `github.com` host → GitHub
- [ ] **2.09** `TestDetectPlatformFromRemote_GitLab` — URL with `gitlab.com` host → GitLab
- [ ] **2.10** `TestDetectPlatformFromRemote_Unknown` — Unknown host returns error
- [ ] **2.11** `TestDetectPRNumber_GitHubActions` — `GITHUB_REF=refs/pull/42/merge` → 42
- [ ] **2.12** `TestDetectPRNumber_GitLabCI` — `CI_MERGE_REQUEST_IID=17` → 17
- [ ] **2.13** `TestDetectPRNumber_NoCIEnv` — No CI env vars → 0 and error
- [ ] **2.14** `TestDetectRepo_GitHubActions` — `GITHUB_REPOSITORY=owner/repo` → `"owner/repo"`
- [ ] **2.15** `TestDetectRepo_GitLabCI` — `CI_PROJECT_PATH=group/project` → `"group/project"`
- [ ] **2.16** `TestCIEnvDetect_NoneDetected` — Clean environment returns nil
- [ ] **2.17** `TestGitHubCLIFallback` — When no token, uses `gh api` for operations
- [ ] **2.18** `TestGitLabCLIFallback` — When no token, uses `glab api` for operations

### Implementation Notes

- Detection order: env vars (`GITHUB_ACTIONS`, `GITLAB_CI`) → git remote URL parsing
- Remote URL parsing supports SSH (`git@host:path`) and HTTPS (`https://host/path`) formats
- GitLab paths may have subgroups (`group/sub/project`) — don't assume single-level

---

## PR 3: Platform Bridge (`internal/platform/bridge.go`)

Connects the **existing** formatters in `internal/security/output/` and `internal/output/` to actual Platform API calls. This is the key integration piece.

### Tests

- [ ] **3.01** `TestBridgePostReviewFromSecurityReport` — Takes `*security.Report`, formats via existing `GitHubPRFormatter`, posts via `Platform.PostPRReview`
- [ ] **3.02** `TestBridgePostReviewEmptyReport` — Empty report posts summary-only comment (no inline comments)
- [ ] **3.03** `TestBridgePostSARIF` — Takes `*security.Report`, formats via existing `SARIFFormatter`, uploads via `Platform.UploadSARIF`
- [ ] **3.04** `TestBridgePostRunResult` — Takes `*output.RunResult`, formats via existing `PRCommentFormatter`, posts via `Platform.PostPRComment`
- [ ] **3.05** `TestBridgeTruncatesLongComments` — Comments exceeding 65000 chars truncated with notice
- [ ] **3.06** `TestBridgeMapsSecurityPRCommentToPlatformReview` — `PRComment{Path, Line, Body}` maps correctly to `platform.ReviewComment`

### Implementation Notes

- The bridge does NOT re-implement formatting. It calls existing `GitHubPRFormatter.Format()` and `SARIFFormatter.Format()`, then maps results to platform API calls.
- For `PostPRReview`: unmarshals `PRReview` JSON from `GitHubPRFormatter`, creates `platform.Review`, calls `Platform.PostPRReview`.
- The bridge is package-level functions, not a type — follows codebase's preference for simple functions.

---

## PR 4: CLI Integration Flags (`cmd/rubichan/main.go`)

Ties everything together with new flags wired into `runHeadless()`.

### New Flags

```bash
--post-to-pr              # Auto-detect platform, format results, post to PR
--pr=N                    # Explicit PR number override
--upload-sarif            # Upload SARIF to GitHub Code Scanning API
```

### Tests

- [ ] **4.01** `TestPostToPRFlagRegistered` — Flag exists and defaults to false
- [ ] **4.02** `TestPRFlagRegistered` — `--pr` flag exists and defaults to 0
- [ ] **4.03** `TestUploadSARIFFlagRegistered` — `--upload-sarif` flag exists and defaults to false
- [ ] **4.04** `TestPostToPRRequiresCodeReviewMode` — `--post-to-pr` without `--mode=code-review` returns clear error
- [ ] **4.05** `TestPostToPRAutoDetectsPlatform` — When `GITHUB_ACTIONS=true`, detects GitHub and PR number
- [ ] **4.06** `TestPostToPRExplicitPRNumber` — `--pr=42` overrides auto-detected PR number
- [ ] **4.07** `TestPostToPRNoPlatformError` — No CI env and no remote URL returns descriptive error
- [ ] **4.08** `TestUploadSARIFPostsToAPI` — Formats SARIF from security report and uploads
- [ ] **4.09** `TestAnnotationsEmittedInGitHubActions` — When `GITHUB_ACTIONS=true`, annotations written to stdout
- [ ] **4.10** `TestPostToPRAndOutputCombined` — `--post-to-pr --output=json` posts to PR AND writes JSON to stdout

### Wiring (after existing formatter switch at ~line 1949 in `runHeadless()`)

1. If `--post-to-pr`: detect platform → resolve PR number → bridge posts review + comment
2. If `--upload-sarif`: format via existing SARIF formatter → `Platform.UploadSARIF`
3. If `GITHUB_ACTIONS=true`: emit annotations via existing `GitHubAnnotationsFormatter`

---

## PR 5: Example CI Workflow Files

### Tests

- [ ] **5.01** `TestExampleGitHubActionsYAMLIsValid` — Parses as valid YAML
- [ ] **5.02** `TestExampleGitLabCIYAMLIsValid` — Parses as valid YAML
- [ ] **5.03** `TestExampleFilesExist` — All three example files exist

### Files

- [ ] **5.04** `examples/ci/github-actions.yml` — GitHub Actions workflow for PR code review
- [ ] **5.05** `examples/ci/gitlab-ci.yml` — GitLab CI pipeline with SAST artifact
- [ ] **5.06** `examples/ci/jenkinsfile` — Jenkins pipeline with SARIF archiving

---

## Dependencies

| Library | Purpose | License |
|---------|---------|---------|
| `google/go-github/v68` | GitHub API client (SDK-first, type-safe) | BSD-3 (spec-approved) |
| `xanzy/go-gitlab` | GitLab API client (SDK-first, type-safe) | Apache-2.0 |
| `golang.org/x/oauth2` | Token transport for go-github (transitive) | BSD-3 |

**NOT needed**: `owenrumney/go-sarif` — custom SARIF implementation already exists at `internal/security/output/sarif.go`.

---

## Implementation Order & Dependencies

```
PR 1: Platform Abstraction Layer (go-github, go-gitlab SDKs)
  |
  v
PR 2: CI Environment Detection (parallel-safe with PR 1)
  |
  +---> PR 3: Platform Bridge (depends on PR 1)
  |
  v
PR 4: CLI Integration Flags (depends on PRs 1-3)
  |
  v
PR 5: Example CI Workflows (depends on PR 4)
```

PRs 1 and 2 can be developed in parallel.

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| GitHub review API rejects comments on lines not in diff | Validate comment positions against PR diff hunk ranges before posting; skip out-of-range with warning |
| GitLab Discussions API requires diff position objects | Map `PRComment.Path` + `PRComment.Line` to GitLab `PositionOptions` using MR diff info from `go-gitlab` |
| Rate limiting on GitHub API | `go-github`'s built-in rate limit handling; batch comments into single review |
| SARIF upload rejects malformed data | Existing `SARIFFormatter` is well-tested; add gzip+base64 encoding |
| Token unavailable in CI | CLI fallback (`gh`/`glab`) which authenticate via their own token caching |
| go-github/go-gitlab API breaking changes | Pin specific versions in go.mod; SDKs are semver-stable |

---

## Spec Alignment

| Spec Requirement | Feature | Status |
|-----------------|---------|--------|
| FR-2.4: SARIF output | Existing `internal/security/output/sarif.go` | **Done** |
| FR-2.4: GitHub PR comment | Existing `internal/output/pr_comment.go` | **Done** |
| FR-2.8: GitHub Actions integration | PRs 1-4 | Planned |
| FR-2.8: GitLab CI integration | PRs 1-4 | Planned |
| FR-2.8: Jenkins integration | PR 5 | Planned |
| FR-4.10: SARIF security output | Existing `internal/security/output/sarif.go` | **Done** |
| FR-4.10: GitHub PR annotations | Existing `internal/output/github_annotations.go` | **Done** |
