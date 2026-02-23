# Milestone 5: Apple Platform + Polish — Design

## Goal

Implement first-class Apple/Xcode development tools as specified in FR-6, assemble the `apple-dev` built-in skill, integrate security findings into wiki output, and write a skill authoring guide.

## Architecture

### New Package: `internal/tools/xcode/`

Each file implements one or more `tools.Tool` conformances. All tools use `exec.CommandContext` directly (not the shell tool) for structured output parsing.

```
internal/tools/xcode/
├── platform.go      # PlatformChecker interface + real/mock
├── discover.go      # xcode_discover — detect project types
├── logparser.go     # Shared xcodebuild output parser
├── xcodebuild.go    # xcode_build, xcode_test, xcode_archive, xcode_clean
├── simctl.go        # sim_list, sim_boot, sim_shutdown, sim_install, sim_launch, sim_screenshot
├── spm.go           # swift_build, swift_test, swift_resolve, swift_add_dep
├── codesign.go      # codesign_info, codesign_verify
└── xcrun.go         # Generic xcrun dispatch
```

### Platform Gating

Platform gating happens at tool registration time, not execution time.

```go
type PlatformChecker interface {
    IsDarwin() bool
    XcodePath() (string, error)  // xcode-select -p
}
```

Injected into all tools for testability. Production uses `runtime.GOOS`; tests use a mock. On non-darwin, only SPM tools (`swift_build`, `swift_test`, `swift_resolve`) are registered. Others return a clear "requires macOS" error if somehow invoked.

### Tool Design

**XcodeBuild (4 sub-tools):**
- `xcode_build`: `{project?, workspace?, scheme, destination, configuration?}` → parsed build result (success/fail + errors/warnings array)
- `xcode_test`: same + `{test_plan?, result_bundle_path?}` → structured test results (pass/fail/skip counts + failure details)
- `xcode_archive`: same + `{archive_path}` → archive path on success
- `xcode_clean`: `{scheme}` → confirmation

All share `logparser.go` which extracts `CompileSwift`, `Ld`, error/warning lines, and test case results into structured Go types.

**Simctl (6 sub-tools):**
- `sim_list` → JSON-decoded device list (runtime, name, UDID, state)
- `sim_boot/shutdown` — `{device}` (name or UDID)
- `sim_install` — `{device, app_path}`
- `sim_launch` — `{device, bundle_id}`
- `sim_screenshot` — `{device, output_path}`

All parse `xcrun simctl` JSON output directly (`simctl list -j`).

**SPM (4 sub-tools, cross-platform):**
- `swift_build`: `{package_path?, configuration?}` — works on macOS AND Linux
- `swift_test`: same → structured test output
- `swift_resolve`: `{package_path?}` → dependency tree
- `swift_add_dep`: `{url, from_version, package_path?}` — modifies Package.swift

**Discovery:**
- `xcode_discover`: `{path?}` → detected project type, schemes, targets

**Code Signing (P1):**
- `codesign_info`: list identities, provisioning profiles, entitlements
- `codesign_verify`: verify code signature of a built app bundle

**Xcrun Dispatch (P1):**
- Generic `xcrun` wrapper for instruments, strings, swift-demangle

### apple-dev Built-in Skill

Lives in `internal/skills/builtin/appledev/`. Multi-type skill combining tools, prompts, and security rules.

**Assembly in `cmd/rubichan/main.go`:**
- On startup, call `xcode.DiscoverProject(cwd)` to check for Apple project files
- If found (or `--skills=apple-dev` explicit), register all Xcode tools with the registry
- Inject Apple system prompt via `agent.WithExtraSystemPrompt(name, content)`
- Apple security scanner already wired — no changes needed

**Prompt:** Single embedded file (`system.md`) via `//go:embed`. Contains Apple platform expertise: build system, signing, Swift concurrency, SwiftUI patterns. Under 5000 tokens.

**Auto-activation trigger:** File pattern match in project root: `.xcodeproj`, `.xcworkspace`, `Package.swift`, `*.swift`. Simple check in `main.go`.

### Wiki-Security Integration

- Add `SecurityReportProvider` interface to `internal/wiki/`
- In wiki command: create security engine, run against project, pass report to assembler
- Assembler formats findings into security page: summary table, per-scanner sections, severity breakdown
- Reuse existing `security/output` markdown formatter where possible
- ~100-150 LOC, no new packages

### Skill Authoring Guide

Create `docs/skill-authoring.md` covering:
- Quick start: create a minimal prompt skill
- Skill types: tool, prompt, workflow, security-rule, transform
- Starlark guide: builtins, SDK functions, sandbox constraints
- Permissions: what each grants, how approval works
- Testing: local testing before publishing
- Registry: publishing, versioning, SemVer ranges

## Deferred Items (P2)

- FR-6.7: Asset catalog management
- FR-6.9: App distribution (altool/notarytool)
- FR-6.11: SwiftUI/UIKit deprecated API detection
- FR-6.12: Xcode project wiki analyzer (needs pbxproj parser)
- FR-6.13: CoreData/SwiftData introspection

## PR Structure

```
PR1 (platform+discover+logparser)
 ├── PR2 (xcodebuild) ──┐
 ├── PR3 (simctl)    ────┤
 ├── PR4 (spm)       ────┼── PR6 (apple-dev skill assembly)
 └── PR5 (codesign)  ────┘
PR7 (wiki-security) ─── independent
PR8 (docs)          ─── independent
```

## Verification

1. `go test ./... -cover` — all packages >90%
2. `go build ./cmd/rubichan` — builds clean on macOS and Linux
3. On macOS with Xcode: `rubichan --headless --prompt "build my project"` uses xcode_build
4. On Linux: only SPM tools registered, clear "requires macOS" message
5. Wiki output includes real security findings page
6. `docs/skill-authoring.md` exists and covers all skill types
