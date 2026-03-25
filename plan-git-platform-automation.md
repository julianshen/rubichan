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
1. **Platform abstraction layer + CI env detection** — SDK clients (`go-github`, `go-gitlab`) and auto-detect platform/repo/PR
2. **Bridge** — connect existing formatters to platform API calls via interfaces
3. **CLI wiring** — new flags in `runHeadless()` to trigger posting
4. **CLI fallback clients** — shell out to `gh`/`glab` when no API token available

---

## Architecture

```
internal/platform/           # NEW — git hosting platform abstraction
├── platform.go              # Platform interface + types
├── github.go                # GitHub impl via google/go-github/v68
├── gitlab.go                # GitLab impl via xanzy/go-gitlab
├── cli_github.go            # GitHub CLI fallback (shells out to `gh`)
├── cli_gitlab.go            # GitLab CLI fallback (shells out to `glab`)
├── cienv.go                 # CI environment detection (env vars, git remote)
├── detect.go                # Client selection: SDK → CLI → error
├── bridge.go                # Connects existing formatters → Platform API calls
├── platform_test.go         # Interface compliance tests
├── github_test.go           # GitHub SDK tests (httptest mocks)
├── gitlab_test.go           # GitLab SDK tests (httptest mocks)
├── cli_github_test.go       # GitHub CLI fallback tests
├── cli_gitlab_test.go       # GitLab CLI fallback tests
├── cienv_test.go            # CI env detection tests
├── detect_test.go           # Client selection tests
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

## PR 1: Platform Abstraction Layer + CI Environment Detection (`internal/platform/`)

Foundation for everything else. Defines interface, GitHub and GitLab implementations using Go SDK libraries, and CI environment detection. CI env detection is included in this PR (not separate) because it's ~100 LOC and the platform layer needs it to be useful.

### Interface Design

```go
// Platform abstracts git hosting platform operations.
type Platform interface {
    Name() string // "github" or "gitlab"

    // Pull/Merge Requests
    PostPRComment(ctx context.Context, repo string, prNumber int, body string) error
    PostPRReview(ctx context.Context, repo string, prNumber int, review Review) error
    GetPRDiff(ctx context.Context, repo string, prNumber int) (string, error)
    ListPRFiles(ctx context.Context, repo string, prNumber int) ([]PRFile, error)

    // CI Integration
    CreateCommitStatus(ctx context.Context, repo string, sha string, status CommitStatus) error
    UploadSARIF(ctx context.Context, repo string, ref string, sarifData []byte) error
}
```

`GetPRDiff` and `ListPRFiles` are needed for the bridge to validate that review comment positions fall within the PR's diff hunks (Risk #1 mitigation). Without these, the bridge cannot filter out-of-range comments.

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

type PRFile struct {
    Filename string
    Status   string // "added", "modified", "removed"
    Patch    string
}

type CommitStatus struct {
    State       string // "success", "failure", "pending"
    Description string
    Context     string
    TargetURL   string
}

type CIEnvironment struct {
    Provider string // "github-actions", "gitlab-ci", "jenkins"
    Repo     string // "owner/repo" or "group/project"
    PRNumber int    // 0 if not a PR context
    Ref      string // git ref
    SHA      string // commit SHA
    Token    string // auth token from env
}
```

### Tests — Platform Clients

- [ ] **1.01** `TestPlatformInterfaceCompliance_GitHub` — `*GitHubClient` satisfies `Platform`
- [ ] **1.02** `TestPlatformInterfaceCompliance_GitLab` — `*GitLabClient` satisfies `Platform`
- [ ] **1.03** `TestGitHubPostPRComment` — Posts comment via `go-github` Issues API (httptest mock)
- [ ] **1.04** `TestGitHubPostPRReview` — Creates review with inline comments via `go-github` (httptest mock)
- [ ] **1.05** `TestGitHubUploadSARIF` — Uploads gzip+base64 SARIF to code-scanning API (httptest mock)
- [ ] **1.06** `TestGitHubUploadSARIF_EncodesGzipBase64` — Verifies request body is gzip-compressed then base64-encoded
- [ ] **1.07** `TestGitHubCreateCommitStatus` — Creates commit status via `go-github` (httptest mock)
- [ ] **1.08** `TestGitHubGetPRDiff` — Retrieves diff via `go-github` PullRequests API (httptest mock)
- [ ] **1.09** `TestGitHubListPRFiles` — Lists changed files via `go-github` (httptest mock)
- [ ] **1.10** `TestGitLabPostMRComment` — Posts MR note via `go-gitlab` (httptest mock)
- [ ] **1.11** `TestGitLabPostMRReview` — Posts MR discussions with position info via `go-gitlab` (httptest mock)
- [ ] **1.12** `TestGitLabUploadSARIF_NoOp` — GitLab SARIF upload is no-op (GitLab ingests via CI artifact)
- [ ] **1.13** `TestGitLabGetMRDiff` — Retrieves MR diff via `go-gitlab` (httptest mock)
- [ ] **1.14** `TestGitLabListMRFiles` — Lists MR changed files via `go-gitlab` (httptest mock)
- [ ] **1.15** `TestGitHubAuthFromToken` — Token creates authenticated `go-github` client
- [ ] **1.16** `TestGitLabAuthFromToken` — Token creates authenticated `go-gitlab` client
- [ ] **1.17** `TestGitHubNewClient_CustomBaseURL` — GitHub Enterprise URL configures `go-github` base URL
- [ ] **1.18** `TestGitLabNewClient_CustomBaseURL` — Self-hosted GitLab URL configures `go-gitlab` base URL

### Tests — CI Environment Detection

- [ ] **1.19** `TestParseRepoFromRemote_GitHubSSH` — `git@github.com:owner/repo.git` → `"owner/repo"`
- [ ] **1.20** `TestParseRepoFromRemote_GitHubHTTPS` — `https://github.com/owner/repo.git` → `"owner/repo"`
- [ ] **1.21** `TestParseRepoFromRemote_GitLabSSH` — `git@gitlab.com:group/project.git` → `"group/project"`
- [ ] **1.22** `TestParseRepoFromRemote_GitLabHTTPS` — `https://gitlab.example.com/group/sub/project` → `"group/sub/project"`
- [ ] **1.23** `TestParseRepoFromRemote_Invalid` — Invalid URL returns error
- [ ] **1.24** `TestDetectPlatformFromEnv_GitHub` — `GITHUB_ACTIONS=true` → GitHub
- [ ] **1.25** `TestDetectPlatformFromEnv_GitLab` — `GITLAB_CI=true` → GitLab
- [ ] **1.26** `TestDetectPlatformFromRemote_GitHub` — URL with `github.com` host → GitHub
- [ ] **1.27** `TestDetectPlatformFromRemote_GitLab` — URL with `gitlab.com` host → GitLab
- [ ] **1.28** `TestDetectPlatformFromRemote_Unknown` — Unknown host returns error
- [ ] **1.29** `TestDetectPRNumber_GitHubActions` — `GITHUB_REF=refs/pull/42/merge` → 42
- [ ] **1.30** `TestDetectPRNumber_GitLabCI` — `CI_MERGE_REQUEST_IID=17` → 17
- [ ] **1.31** `TestDetectPRNumber_NoCIEnv` — No CI env vars → 0 and error
- [ ] **1.32** `TestDetectRepo_GitHubActions` — `GITHUB_REPOSITORY=owner/repo` → `"owner/repo"`
- [ ] **1.33** `TestDetectRepo_GitLabCI` — `CI_PROJECT_PATH=group/project` → `"group/project"`
- [ ] **1.34** `TestCIEnvDetect_NoneDetected` — Clean environment returns nil

### Implementation Notes

- **GitHub**: Wrap `google/go-github/v68`. `PostPRReview` maps `Review.Comments` to `github.DraftReviewComment` entries. `UploadSARIF` uses `POST /repos/{owner}/{repo}/code-scanning/sarifs` with gzip+base64 encoding as GitHub requires. `GetPRDiff` uses `PullRequests.GetRaw()` with `github.RawOptions{Type: github.Diff}`.
- **GitLab**: Wrap `xanzy/go-gitlab`. For inline comments, use MR Discussions API with `PositionOptions`. `UploadSARIF` is a no-op — GitLab ingests SARIF as a CI artifact, not via API. `GetMRDiff` uses `MergeRequests.GetMergeRequestDiffVersions()`.
- **Auth**: Constructor accepts token directly; caller reads from env. No hidden env var coupling in the client.
- **CI Detection**: Env vars (`GITHUB_ACTIONS`, `GITLAB_CI`) checked first, then git remote URL parsing. GitLab paths support subgroups (`group/sub/project`).

---

## PR 2: Platform Bridge (`internal/platform/bridge.go`)

Connects the **existing** formatters in `internal/security/output/` and `internal/output/` to actual Platform API calls. This is the key integration piece.

The bridge accepts **interfaces**, not concrete formatter types:
- `security.OutputFormatter` (has `Name() string` + `Format(*Report) ([]byte, error)`)
- `output.Formatter` (has `Format(*RunResult) ([]byte, error)`)

This allows swapping formatters (e.g., using `SARIFFormatter` vs `CycloneDXFormatter`) without touching the bridge.

### Bridge Function Signatures

```go
// PostSecurityReview formats a security report and posts it as a PR review.
// Uses Platform.GetPRDiff to validate comment positions fall within diff hunks.
func PostSecurityReview(
    ctx context.Context,
    p Platform,
    formatter security.OutputFormatter,
    report *security.Report,
    repo string,
    prNumber int,
) error

// UploadSecuritySARIF formats a security report as SARIF and uploads it.
func UploadSecuritySARIF(
    ctx context.Context,
    p Platform,
    formatter security.OutputFormatter,
    report *security.Report,
    repo string,
    ref string,
) error

// PostRunResultComment formats a run result and posts it as a PR comment.
func PostRunResultComment(
    ctx context.Context,
    p Platform,
    formatter output.Formatter,
    result *output.RunResult,
    repo string,
    prNumber int,
) error
```

### Tests

- [ ] **2.01** `TestBridgePostReviewFromSecurityReport` — Takes `*security.Report`, formats via `security.OutputFormatter` interface, posts via `Platform.PostPRReview`
- [ ] **2.02** `TestBridgePostReviewEmptyReport` — Empty report posts summary-only comment (no inline comments)
- [ ] **2.03** `TestBridgePostReviewValidatesPositions` — Uses `Platform.GetPRDiff` to filter comments outside diff hunks, logs warnings for skipped comments
- [ ] **2.04** `TestBridgePostSARIF` — Takes `*security.Report`, formats via `security.OutputFormatter`, uploads via `Platform.UploadSARIF`
- [ ] **2.05** `TestBridgePostRunResult` — Takes `*output.RunResult`, formats via `output.Formatter` interface, posts via `Platform.PostPRComment`
- [ ] **2.06** `TestBridgeTruncatesLongComments` — Comments exceeding 65000 chars truncated with notice
- [ ] **2.07** `TestBridgeMapsSecurityPRCommentToPlatformReview` — `PRComment{Path, Line, Body}` maps correctly to `platform.ReviewComment`
- [ ] **2.08** `TestBridgeAcceptsAnyOutputFormatter` — Works with any `security.OutputFormatter` implementation (not just `GitHubPRFormatter`)
- [ ] **2.09** `TestBridgeAcceptsAnyFormatter` — Works with any `output.Formatter` implementation (not just `PRCommentFormatter`)

### Implementation Notes

- The bridge does NOT re-implement formatting. It calls the passed formatter's `Format()` method, then maps results to platform API calls.
- For `PostSecurityReview`: unmarshals `PRReview` JSON from the formatter output, fetches PR diff via `Platform.GetPRDiff`, filters comments whose `Path`+`Line` fall outside diff hunks, creates `platform.Review`, calls `Platform.PostPRReview`.
- The bridge is package-level functions, not a type — follows codebase's preference for simple functions over unnecessary structs.

---

## PR 3: CLI Integration Flags (`cmd/rubichan/main.go`)

Ties everything together with new flags wired into `runHeadless()`.

### New Flags

```bash
--post-to-pr              # Auto-detect platform, format results, post to PR
--pr=N                    # Explicit PR number override
--upload-sarif            # Upload SARIF to GitHub Code Scanning API
```

### Tests

- [ ] **3.01** `TestPostToPRFlagRegistered` — Flag exists and defaults to false
- [ ] **3.02** `TestPRFlagRegistered` — `--pr` flag exists and defaults to 0
- [ ] **3.03** `TestUploadSARIFFlagRegistered` — `--upload-sarif` flag exists and defaults to false
- [ ] **3.04** `TestPostToPRRequiresCodeReviewMode` — `--post-to-pr` without `--mode=code-review` returns clear error
- [ ] **3.05** `TestPostToPRAutoDetectsPlatform` — When `GITHUB_ACTIONS=true`, detects GitHub and PR number
- [ ] **3.06** `TestPostToPRExplicitPRNumber` — `--pr=42` overrides auto-detected PR number
- [ ] **3.07** `TestPostToPRNoPlatformError` — No CI env and no remote URL returns descriptive error
- [ ] **3.08** `TestUploadSARIFPostsToAPI` — Formats SARIF from security report and uploads
- [ ] **3.09** `TestAnnotationsEmittedInGitHubActions` — When `GITHUB_ACTIONS=true`, annotations written to stdout
- [ ] **3.10** `TestPostToPRAndOutputCombined` — `--post-to-pr --output=json` posts to PR AND writes JSON to stdout
- [ ] **3.11** `TestPostToPRNoTokenAndNoCLI` — Neither token nor `gh`/`glab` installed returns clear error with setup instructions

### Wiring (after existing formatter switch at ~line 1949 in `runHeadless()`)

1. If `--post-to-pr`: detect platform → resolve PR number → bridge posts review + comment
2. If `--upload-sarif`: format via existing SARIF formatter → `Platform.UploadSARIF`
3. If `GITHUB_ACTIONS=true`: emit annotations via existing `GitHubAnnotationsFormatter`

---

## PR 4: CLI Fallback Clients (`internal/platform/cli_github.go`, `internal/platform/cli_gitlab.go`)

Provides fallback Platform implementations that shell out to `gh` and `glab` CLI tools when API tokens are unavailable. This is a **separate PR** from the SDK clients to keep PR 1 focused and because CLI fallback is a secondary code path.

### Design

```go
// CLIGitHubClient implements Platform by shelling out to `gh api`.
type CLIGitHubClient struct {
    execFn func(ctx context.Context, args ...string) ([]byte, error)
}

// CLIGitLabClient implements Platform by shelling out to `glab api`.
type CLIGitLabClient struct {
    execFn func(ctx context.Context, args ...string) ([]byte, error)
}
```

Both follow the existing `git_runner.go` pattern: constructor injection of an exec function for testability.

### CLI Availability Strategy

1. Check for API token (e.g., `GITHUB_TOKEN`). If present → use SDK client (PR 1).
2. If no token, check if CLI tool is installed: `gh --version` / `glab --version`.
3. If CLI is installed → use CLI client. CLI tools authenticate via their own token caching (`gh auth login`).
4. If neither token nor CLI → return descriptive error: `"GitHub token not found and 'gh' CLI not installed. Set GITHUB_TOKEN or install gh: https://cli.github.com"`.

### Supported Operations via CLI

All `Platform` interface methods are supported:
- `PostPRComment` → `gh api repos/{repo}/issues/{pr}/comments -f body=...`
- `PostPRReview` → `gh api repos/{repo}/pulls/{pr}/reviews -X POST --input -`
- `GetPRDiff` → `gh pr diff {pr} --repo {repo}`
- `ListPRFiles` → `gh api repos/{repo}/pulls/{pr}/files`
- `UploadSARIF` → `gh api repos/{repo}/code-scanning/sarifs -X POST --input -`
- `CreateCommitStatus` → `gh api repos/{repo}/statuses/{sha} -X POST --input -`

GitLab equivalents use `glab api` with corresponding GitLab API paths.

### Tests

- [ ] **4.01** `TestCLIGitHubClientImplementsPlatform` — `*CLIGitHubClient` satisfies `Platform`
- [ ] **4.02** `TestCLIGitLabClientImplementsPlatform` — `*CLIGitLabClient` satisfies `Platform`
- [ ] **4.03** `TestCLIGitHubPostPRComment` — Executes correct `gh api` command with args
- [ ] **4.04** `TestCLIGitHubPostPRReview` — Passes review JSON to `gh api` via stdin
- [ ] **4.05** `TestCLIGitHubGetPRDiff` — Parses `gh pr diff` output
- [ ] **4.06** `TestCLIGitLabPostMRComment` — Executes correct `glab api` command
- [ ] **4.07** `TestCLIClientDetection_TokenAvailable` — Returns SDK client when token present
- [ ] **4.08** `TestCLIClientDetection_CLIAvailable` — Returns CLI client when no token but CLI installed
- [ ] **4.09** `TestCLIClientDetection_NeitherAvailable` — Returns descriptive error with install instructions
- [ ] **4.10** `TestCLIClientExecFailure` — CLI error propagated with stderr context

### Implementation Notes

- Use `exec.CommandContext` for CLI calls, matching `git_runner.go` pattern
- Inject exec function via constructor for testability (no real CLI in unit tests)
- Parse JSON responses from `gh api` / `glab api` using standard `encoding/json`
- `GetPRDiff` via CLI uses `gh pr diff` which returns raw diff text directly

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
PR 1: Platform Abstraction + CI Env Detection (go-github, go-gitlab SDKs)
  |
  v
PR 2: Platform Bridge (depends on PR 1)
  |
  v
PR 3: CLI Integration Flags (depends on PRs 1-2)
  |
  +---> PR 4: CLI Fallback Clients (depends on PR 1, independent of PRs 2-3)
  |
  v
PR 5: Example CI Workflows (depends on PR 3)
```

PR 4 (CLI fallback) can be developed in parallel with PRs 2-3, since it only depends on the Platform interface from PR 1.

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| GitHub review API rejects comments on lines not in diff | Bridge calls `Platform.GetPRDiff` to validate comment positions against diff hunk ranges; skips out-of-range with warning |
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
| FR-2.8: GitHub Actions integration | PRs 1-3 | Planned |
| FR-2.8: GitLab CI integration | PRs 1-3 | Planned |
| FR-2.8: Jenkins integration | PR 5 | Planned |
| FR-4.10: SARIF security output | Existing `internal/security/output/sarif.go` | **Done** |
| FR-4.10: GitHub PR annotations | Existing `internal/output/github_annotations.go` | **Done** |
