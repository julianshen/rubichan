# Security Engine Design

**Goal:** Build the complete security analysis engine (`internal/security`) — a standalone, two-phase hybrid system with static scanners, LLM-powered analyzers, risk-based prioritization, attack chain correlation, and six output formats.

**Spec references:** Section 3.7 (Security Engine), ADR-003, ADR-004, Appendix C (Finding Schema)

---

## Architecture

Bottom-up layered build. Each layer depends only on layers below it:

```
Layer 6: Engine Orchestrator (wires everything, runs the pipeline)
Layer 5: Output Formatters (JSON, Markdown, SARIF, GitHub PR, Wiki, CycloneDX)
Layer 4: Correlator (attack chain detection, deduplication)
Layer 3: LLM Analyzers (auth, dataflow, business, crypto, concurrency)
Layer 2: Prioritizer (risk scoring, chunk selection, budget management)
Layer 1: Static Scanners (secrets, deps, SAST, config, license, Apple)
Layer 0: Types & Interfaces (Finding, Severity, Category, interfaces)
```

The engine is consumed as a library by code review, wiki generator, standalone audit, and CI gating. Security Rule Skills extend both phases via adapter types.

## Layer 0: Types & Interfaces

**Package:** `internal/security`

### Core Types

- `Severity` — string enum: `Critical`, `High`, `Medium`, `Low`, `Info`
- `Confidence` — string enum: `High`, `Medium`, `Low`
- `Category` — string enum for 13 categories: `injection`, `authentication`, `authorization`, `cryptography`, `secrets-exposure`, `vulnerable-dependency`, `misconfiguration`, `data-exposure`, `race-condition`, `input-validation`, `logging-monitoring`, `supply-chain`, `license-compliance`
- `Location` — `{ File, StartLine, EndLine, Function }`
- `Finding` — unified schema from Appendix C: ID, Scanner, Severity, Category, Title, Description, Location, CWE, OWASP, Evidence, Remediation, Confidence, References, Metadata, SkillSource
- `AttackChain` — `{ ID, Title, Severity, Steps []Finding, Impact, Likelihood }`
- `ScanTarget` — `{ RootDir, Files []string, ExcludePatterns []string }`
- `AnalysisChunk` — `{ File, StartLine, EndLine, Content, Language, RiskScore }`
- `Report` — `{ Findings []Finding, AttackChains []AttackChain, Stats ScanStats, Errors []ScanError }`
- `ScanStats` — `{ Duration, FilesScanned, ChunksAnalyzed, FindingsCount, ChainCount }`
- `ScanError` — `{ Scanner, Error, Fatal bool }`

### Interfaces

```go
type StaticScanner interface {
    Name() string
    Scan(ctx context.Context, target ScanTarget) ([]Finding, error)
}

type LLMAnalyzer interface {
    Name() string
    Category() Category
    Analyze(ctx context.Context, chunks []AnalysisChunk) ([]Finding, error)
}

type OutputFormatter interface {
    Name() string
    Format(report *Report) ([]byte, error)
}
```

**Files:** `internal/security/types.go`, `internal/security/types_test.go`

## Layer 1: Static Scanners

**Package:** `internal/security/scanner`

| Scanner | File | Technique | Key Rules |
|---------|------|-----------|-----------|
| Secret Scanner | `secrets.go` | Regex + Shannon entropy | AWS keys, GCP keys, GitHub/GitLab tokens, Slack tokens, JWT secrets, private keys (RSA/EC/DSA), DB connection strings, generic high-entropy, Bearer/Basic auth patterns |
| Dependency Auditor | `deps.go` | Lockfile parsing + OSV API | go.sum, package-lock.json, requirements.txt, Gemfile.lock, Cargo.lock, Podfile.lock queries against OSV database |
| SAST Pattern Matcher | `sast.go` | Tree-sitter AST queries | SQL injection (string concat in queries), path traversal, XSS (unescaped templates), command injection (os/exec with user input), weak crypto (MD5/SHA1/DES/RC4), hardcoded credentials |
| Config Scanner | `config.go` | File-type-specific rules | Dockerfile USER root, K8s privileged/hostNetwork, CI secrets in plain text, permissive CORS, debug mode |
| License Checker | `license.go` | License file + header detection | GPL in commercial, missing licenses, copyleft in permissive projects |
| Apple Platform Scanner | `apple.go` | Info.plist XML + entitlements | ATS exceptions, insecure UserDefaults, missing privacy keys, excessive entitlements |

Plus `skill_scanner.go` — adapter wrapping skill-provided scanners into `StaticScanner`.

## Layer 2: Prioritizer

**File:** `internal/security/prioritizer.go`

Takes `ScanTarget` + Phase 1 findings, produces scored `[]AnalysisChunk`.

Risk signal scoring (from spec):

| Signal | Score |
|--------|-------|
| Auth code (auth, login, jwt, session) | +10 |
| Command execution (os/exec) | +9 |
| Input handling (HTTP handlers) | +8 |
| Database access (sql, orm) | +7 |
| Crypto operations | +7 |
| Keychain / security framework | +6 |
| File operations | +5 |
| Network / WebView code | +5 |
| Already flagged by static scanners | +3 |
| Recently modified (git) | +2 |

Files are split into analysis chunks respecting function boundaries. Chunks below `MinRiskScore` are skipped. Total chunks capped by `MaxLLMChunks`.

## Layer 3: LLM Analyzers

**Package:** `internal/security/analyzer`

Five analyzers, each implementing `LLMAnalyzer`. Each uses `provider.LLMProvider` (existing interface) for LLM calls.

| Analyzer | File | Prompt Focus |
|----------|------|-------------|
| Auth/Authz | `auth.go` | Authentication bypass, IDOR, privilege escalation, missing middleware |
| Data Flow / Taint | `dataflow.go` | Untrusted input reaching dangerous sinks (SQL, exec, file ops) |
| Business Logic | `business.go` | Logic flaws (negative quantities, race-to-credit, bypass conditions) |
| Cryptography | `crypto.go` | Weak algorithms, key management, ECB mode, hardcoded keys |
| Concurrency | `concurrency.go` | Race conditions, deadlocks, concurrent map access |

Each analyzer:
1. Receives `[]AnalysisChunk` filtered to its category
2. Builds a system prompt with CWE/OWASP context
3. Sends chunks to LLM via `provider.LLMProvider.Stream()`
4. Parses response as structured JSON `[]Finding`
5. Assigns `Confidence` based on LLM certainty signals

Plus `skill_analyzer.go` — adapter for skill-provided analyzers.

## Layer 4: Correlator

**File:** `internal/security/correlator.go`

Takes all findings (static + LLM) and identifies attack chains.

Strategy:
- Group findings by file/function proximity
- Match known attack chain patterns (e.g., missing auth + SQL injection = "Unauthenticated SQL Injection")
- Chain severity = max component severity, promoted if chain creates new attack surface
- Deduplicate overlapping findings (same CWE + same location from different scanners)

## Layer 5: Output Formatters

**Package:** `internal/security/output`

| Formatter | File | Output | Use Case |
|-----------|------|--------|----------|
| JSON | `json.go` | Structured JSON | Programmatic consumption, piping |
| Markdown | `markdown.go` | Human-readable report with severity badges, tables | Terminal/file reading |
| SARIF | `sarif.go` | SARIF v2.1.0 JSON | IDE integration, GitHub Code Scanning |
| GitHub PR | `github_pr.go` | Positioned review comments | PR review integration |
| Wiki | `wiki.go` | Markdown pages for docs/wiki/security/ | Wiki generator pipeline |
| CycloneDX | `cyclonedx.go` | CycloneDX v1.5 BOM with vulnerabilities | SBOM compliance |

SARIF and CycloneDX follow their respective JSON schemas. GitHub PR formatter produces structured data; the caller translates to API calls.

## Layer 6: Engine Orchestrator

**File:** `internal/security/engine.go`

```go
type Engine struct {
    scanners    []StaticScanner
    analyzers   []LLMAnalyzer
    prioritizer *Prioritizer
    correlator  *Correlator
    provider    provider.LLMProvider
    config      EngineConfig
}

type EngineConfig struct {
    MaxLLMChunks    int      // budget cap for LLM analysis
    MinRiskScore    int      // threshold for LLM analysis
    ExcludePatterns []string // glob patterns to skip
    Concurrency     int      // max parallel scanners/analyzers
}
```

`Engine.Run(ctx, ScanTarget) -> *Report`:
1. Run all static scanners concurrently (`sourcegraph/conc` errgroup)
2. Collect Phase 1 findings
3. Feed findings + target to Prioritizer -> scored chunks
4. Run LLM analyzers concurrently (respecting budget cap)
5. Collect Phase 2 findings
6. Feed all findings to Correlator -> attack chains + deduplicated findings
7. Return `Report`

## Error Handling

- Individual scanner/analyzer failures don't abort the engine — errors collected in `Report.Errors`
- LLM parsing failures (malformed JSON) produce a Finding with `Confidence: Low` and raw response in Evidence
- OSV API unavailability degrades gracefully (Info-level finding noting the limitation)
- Budget exhaustion stops LLM analysis but returns all findings collected so far

## Testing Strategy

- Each scanner: unit tests with fixture files containing known vulnerabilities
- Each analyzer: unit tests with mock LLM provider returning canned JSON
- Prioritizer: unit tests with varied risk signal combinations
- Correlator: unit tests with known attack chain patterns
- Engine: integration test running full pipeline on a test fixture directory
- Output formatters: golden file tests comparing output to expected files

## File Layout

```
internal/security/
├── types.go              # Finding, Severity, Category, interfaces
├── types_test.go
├── engine.go             # Engine orchestrator
├── engine_test.go
├── prioritizer.go        # Risk scoring and chunk selection
├── prioritizer_test.go
├── correlator.go         # Attack chain detection
├── correlator_test.go
├── scanner/
│   ├── secrets.go        # Secret scanner
│   ├── secrets_test.go
│   ├── deps.go           # Dependency auditor
│   ├── deps_test.go
│   ├── sast.go           # SAST pattern matcher
│   ├── sast_test.go
│   ├── config.go         # Config scanner
│   ├── config_test.go
│   ├── license.go        # License checker
│   ├── license_test.go
│   ├── apple.go          # Apple platform scanner
│   ├── apple_test.go
│   └── skill_scanner.go  # Skill-provided scanner adapter
├── analyzer/
│   ├── auth.go           # Auth/Authz analyzer
│   ├── auth_test.go
│   ├── dataflow.go       # Data flow / taint analyzer
│   ├── dataflow_test.go
│   ├── business.go       # Business logic analyzer
│   ├── business_test.go
│   ├── crypto.go         # Cryptography analyzer
│   ├── crypto_test.go
│   ├── concurrency.go    # Concurrency analyzer
│   ├── concurrency_test.go
│   └── skill_analyzer.go # Skill-provided analyzer adapter
└── output/
    ├── json.go           # JSON formatter
    ├── json_test.go
    ├── markdown.go       # Markdown formatter
    ├── markdown_test.go
    ├── sarif.go          # SARIF v2.1.0 formatter
    ├── sarif_test.go
    ├── github_pr.go      # GitHub PR comment formatter
    ├── github_pr_test.go
    ├── wiki.go           # Wiki section formatter
    ├── wiki_test.go
    ├── cyclonedx.go      # CycloneDX formatter
    └── cyclonedx_test.go
```
