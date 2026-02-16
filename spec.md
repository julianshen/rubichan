# AI Coding Agent — Product Specification & Design Document

> **Version:** 1.1 · **Date:** February 2026 · **Status:** Draft  
> **Language:** Go · **License:** Open Source

---

## Table of Contents

- [1. Executive Summary](#1-executive-summary)
- [2. Product Requirements](#2-product-requirements)
  - [2.1 User Personas](#21-user-personas)
  - [2.2 Functional Requirements](#22-functional-requirements)
  - [2.3 Non-Functional Requirements](#23-non-functional-requirements)
- [3. System Architecture](#3-system-architecture)
  - [3.1 Architecture Overview](#31-architecture-overview)
  - [3.2 Execution Modes](#32-execution-modes)
  - [3.3 Agent Core](#33-agent-core)
  - [3.4 Tool Layer](#34-tool-layer)
  - [3.5 LLM Provider Layer](#35-llm-provider-layer)
  - [3.6 Agent Skills System](#36-agent-skills-system)
  - [3.7 Security Analysis Engine](#37-security-analysis-engine)
  - [3.8 Wiki Generator Pipeline](#38-wiki-generator-pipeline)
  - [3.9 Config & Storage](#39-config--storage)
- [4. Agent Skills System — Detailed Design](#4-agent-skills-system--detailed-design)
  - [4.1 Skill Types](#41-skill-types)
  - [4.2 Skill Manifest (SKILL.yaml)](#42-skill-manifest-skillyaml)
  - [4.3 Skill Sources & Discovery](#43-skill-sources--discovery)
  - [4.4 Skill Runtime](#44-skill-runtime)
  - [4.5 Skill Permissions & Sandboxing](#45-skill-permissions--sandboxing)
  - [4.6 Skill Lifecycle Hooks](#46-skill-lifecycle-hooks)
  - [4.7 Skill Triggers & Auto-Activation](#47-skill-triggers--auto-activation)
  - [4.8 Skill SDK](#48-skill-sdk)
  - [4.9 Skill Composition & Dependencies](#49-skill-composition--dependencies)
  - [4.10 Built-in Skills](#410-built-in-skills)
  - [4.11 Example Skills](#411-example-skills)
  - [4.12 Skill Registry & Distribution](#412-skill-registry--distribution)
- [5. Project Structure](#5-project-structure)
- [6. Key Interface Definitions](#6-key-interface-definitions)
- [7. Technology Reference](#7-technology-reference)
  - [7.1 Core Go Libraries](#71-core-go-libraries)
  - [7.2 Security-Specific Libraries](#72-security-specific-libraries)
  - [7.3 Skills-Specific Libraries](#73-skills-specific-libraries)
  - [7.4 External Tools (Optional Runtime)](#74-external-tools-optional-runtime)
  - [7.5 LLM Provider APIs](#75-llm-provider-apis)
- [8. Architecture Decision Records](#8-architecture-decision-records)
  - [ADR-001: Go as Implementation Language](#adr-001-go-as-the-implementation-language)
  - [ADR-002: Shared Agent Core Across All Modes](#adr-002-shared-agent-core-across-all-modes)
  - [ADR-003: Security Engine as Standalone Subsystem](#adr-003-security-engine-as-a-standalone-subsystem)
  - [ADR-004: Static + LLM Hybrid Security](#adr-004-static--llm-hybrid-approach-for-security)
  - [ADR-005: Mermaid as Default Diagram Format](#adr-005-mermaid-as-default-diagram-format)
  - [ADR-006: No Vendor LLM SDKs](#adr-006-no-vendor-llm-sdks)
  - [ADR-007: Tree-sitter for Multi-Language Code Analysis](#adr-007-tree-sitter-for-multi-language-code-analysis)
  - [ADR-008: Starlark as Skill Scripting Language](#adr-008-starlark-as-skill-scripting-language)
  - [ADR-009: Skill Permission Model](#adr-009-skill-permission-model)
  - [ADR-010: Xcode CLI as Built-in Skill with Platform Gating](#adr-010-xcode-cli-as-built-in-skill-with-platform-gating)
- [9. Implementation Roadmap](#9-implementation-roadmap)
- [10. Risk Assessment](#10-risk-assessment)
- [Appendix A: CLI Command Reference](#appendix-a-cli-command-reference)
- [Appendix B: Configuration Reference](#appendix-b-configuration-reference)
- [Appendix C: Unified Finding Schema](#appendix-c-unified-finding-schema)
- [Appendix D: Wiki Output Structure](#appendix-d-wiki-output-structure)
- [Appendix E: Skill Manifest Reference](#appendix-e-skill-manifest-reference)
- [Appendix F: Skill SDK API Reference](#appendix-f-skill-sdk-api-reference)
- [Appendix G: Xcode Tool JSON Schemas](#appendix-g-xcode-tool-json-schemas)

---

## 1. Executive Summary

This document specifies the product requirements, architectural design, technology decisions, and implementation plan for an open-source, Go-based AI Coding Agent. The agent is a terminal-first tool that leverages large language models to assist software engineers with interactive coding, automated code review, security analysis, and codebase documentation generation.

The product operates in three primary modes:

- **Interactive Mode:** A rich terminal UI (TUI) for conversational coding, file editing, shell execution, and code search. Equivalent in capability to Anthropic's Claude Code or OpenAI's Codex CLI.
- **Headless Mode:** A non-interactive, pipe-friendly interface for CI/CD integration. Supports automated code review on pull requests and SRE log analysis. Outputs structured formats (JSON, SARIF, Markdown, GitHub PR comments).
- **Wiki Generator Mode:** A batch pipeline that analyzes an entire codebase and produces a static documentation site with architecture diagrams, module documentation, test reports, security audit, and improvement suggestions.

The agent is designed as an **extensible platform** through a skill system. Skills are pluggable units that add new tools, domain knowledge, multi-step workflows, security rules, and output transforms. Skills can be bundled, project-local, user-global, or installed from a community registry.

All three modes share a common Agent Core, Tool Layer, Skill Runtime, and Security Analysis Engine, ensuring consistent behavior and reducing maintenance burden.

The agent includes first-class support for Apple platform development via Xcode CLI tools (`xcodebuild`, `xcrun simctl`, Swift Package Manager, code signing), with iOS/macOS-specific security scanning rules and SwiftUI/UIKit domain knowledge.

---

## 2. Product Requirements

### 2.1 User Personas

| Persona | Description | Primary Modes |
|---------|-------------|---------------|
| Software Engineer | Day-to-day coding, refactoring, feature development | Interactive |
| Tech Lead / Reviewer | Code review, architecture assessment, quality gates | Headless (CI), Wiki |
| SRE / DevOps Engineer | Incident response, log analysis, infrastructure review | Headless (CLI pipe) |
| Security Engineer | Vulnerability assessment, audit, compliance | Security Audit, Wiki |
| New Team Member | Onboarding, codebase understanding | Wiki, Interactive |
| Apple Platform Dev | iOS/macOS app development, builds, testing, signing | Interactive, Headless |
| Skill Author | Builds and distributes agent extensions | Interactive, SDK |

### 2.2 Functional Requirements

#### FR-1: Interactive Mode

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-1.1 | Rich TUI with streaming LLM response rendering, syntax-highlighted code blocks, and diff previews | P0 |
| FR-1.2 | File read, write, and surgical patch (str_replace-style) operations with user approval gate | P0 |
| FR-1.3 | Shell command execution with timeout, sandboxing, and output capture | P0 |
| FR-1.4 | Code search via regex (ripgrep) and AST-aware queries (tree-sitter) | P0 |
| FR-1.5 | Git operations: diff, status, commit, branch, log | P1 |
| FR-1.6 | LSP integration for diagnostics, go-to-definition, and completions | P2 |
| FR-1.7 | MCP (Model Context Protocol) client for extensible tool connectivity | P1 |
| FR-1.8 | Session persistence and conversation history with resume capability | P1 |
| FR-1.9 | Multi-provider LLM support (Anthropic, OpenAI, Ollama) with streaming | P0 |
| FR-1.10 | Context window management with automatic truncation and summarization | P0 |

#### FR-2: Headless Mode

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-2.1 | Accept input via `--prompt` flag, `--file` flag, or stdin pipe | P0 |
| FR-2.2 | Support `--mode=code-review` with automatic git diff extraction and structured findings output | P0 |
| FR-2.3 | Support `--mode=log-analysis` with stdin piping and structured incident reports | P0 |
| FR-2.4 | Output formats: JSON, Markdown, SARIF, GitHub PR comment | P0 |
| FR-2.5 | Configurable tool whitelist (`--tools=read,search`) to restrict agent capabilities | P1 |
| FR-2.6 | Max-turns and timeout controls for deterministic CI execution | P0 |
| FR-2.7 | Exit code control: exit 1 when findings exceed severity/count thresholds | P0 |
| FR-2.8 | GitHub Actions, GitLab CI, and Jenkins integration examples and documentation | P1 |

#### FR-3: Wiki Generator

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-3.1 | Codebase scanning with tree-sitter AST extraction for all major languages | P0 |
| FR-3.2 | Multi-pass LLM analysis: per-module summarization, cross-cutting synthesis, targeted deep-dives | P0 |
| FR-3.3 | Diagram generation: architecture overview, dependency graphs, data flow, sequence diagrams | P0 |
| FR-3.4 | Output document structure: overview, code structure, module pages, test reports, suggestions | P0 |
| FR-3.5 | Static site output: Hugo, MkDocs, Docusaurus, or raw Markdown | P1 |
| FR-3.6 | Diagram format: Mermaid (default), D2, or Graphviz DOT | P1 |
| FR-3.7 | Incremental regeneration: only re-analyze changed modules since last run | P2 |
| FR-3.8 | Test execution integration: optionally run tests and include results in wiki | P1 |

#### FR-4: Security Analysis Engine

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-4.1 | Secret/credential detection via regex patterns and Shannon entropy verification | P0 |
| FR-4.2 | Dependency vulnerability audit via OSV database (go.sum, package-lock.json, Cargo.lock, etc.) | P0 |
| FR-4.3 | SAST pattern matching via tree-sitter AST queries (SQL injection, path traversal, weak crypto, etc.) | P0 |
| FR-4.4 | Infrastructure/config scanning: Dockerfile, Kubernetes YAML, CI configs, Terraform | P1 |
| FR-4.5 | LLM-powered auth/authz flow analysis across multiple files | P0 |
| FR-4.6 | LLM-powered taint/data-flow tracking from untrusted sources to sensitive sinks | P0 |
| FR-4.7 | LLM-powered business logic vulnerability detection | P1 |
| FR-4.8 | Attack chain correlation: combine related findings into exploit paths | P1 |
| FR-4.9 | Custom rules via `.security.yaml` (project-specific patterns, severity overrides, dependency bans) | P1 |
| FR-4.10 | Output: SARIF, JSON, Markdown, GitHub PR annotations, Wiki security chapter, CycloneDX SBOM | P0 |
| FR-4.11 | License compliance checking for all declared dependencies | P2 |
| FR-4.12 | Smart LLM budget prioritization: score files by risk signals, analyze highest-risk first | P1 |

#### FR-5: Agent Skills System

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-5.1 | Skill manifest format (`SKILL.yaml`) defining metadata, type, triggers, permissions, and dependencies | P0 |
| FR-5.2 | Five skill types: Tool, Prompt, Workflow, Security Rule, Transform | P0 |
| FR-5.3 | Skill discovery from four sources: built-in, project-local (`.agent/skills/`), user-global (`~/.config/aiagent/skills/`), and registry | P0 |
| FR-5.4 | Auto-activation: skills activate based on trigger conditions (file patterns, language detection, CLI flags, conversation keywords) | P0 |
| FR-5.5 | Skill permission model: each skill declares required permissions; user approves on first use | P0 |
| FR-5.6 | Three implementation backends: Go plugin (`.so`), Starlark script (embedded), external process (any language via JSON-RPC) | P1 |
| FR-5.7 | Skill lifecycle hooks: `on_activate`, `on_deactivate`, `on_conversation_start`, `on_before_tool_call`, `on_after_response` | P1 |
| FR-5.8 | Skill composition: skills can declare dependencies on other skills | P1 |
| FR-5.9 | CLI commands for skill management: `aiagent skill list`, `install`, `remove`, `create`, `info` | P0 |
| FR-5.10 | Skill SDK with typed Go API and Starlark helper library | P1 |
| FR-5.11 | Skill isolation: skills cannot access resources beyond their declared permissions | P0 |
| FR-5.12 | Skills work across all execution modes (interactive, headless, wiki) | P0 |
| FR-5.13 | MCP servers auto-discovered as skills | P2 |

#### FR-6: Apple Platform Development (Xcode CLI)

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-6.1 | Xcode project discovery: detect `.xcodeproj`, `.xcworkspace`, `Package.swift`, and SPM projects automatically | P0 |
| FR-6.2 | Build operations via `xcodebuild`: build, clean, test, archive with scheme/destination/configuration selection | P0 |
| FR-6.3 | Simulator management via `xcrun simctl`: list, boot, shutdown, install, launch, screenshot, video recording | P0 |
| FR-6.4 | Swift Package Manager operations: resolve, build, test, add/remove dependencies | P0 |
| FR-6.5 | Code signing introspection: list identities, provisioning profiles, entitlements; diagnose signing errors | P1 |
| FR-6.6 | Build log parsing: extract errors, warnings, and test failures from `xcodebuild` output into structured findings | P0 |
| FR-6.7 | Asset catalog management: list image sets, app icons, color sets; detect missing or unused assets | P2 |
| FR-6.8 | `xcrun` tool dispatch: instruments, strings verification, swift-demangle, sourcekit-lsp | P1 |
| FR-6.9 | App distribution support: `xcodebuild -exportArchive`, `xcrun altool` / `xcrun notarytool` for notarization | P2 |
| FR-6.10 | iOS/macOS-specific security scanning: ATS exceptions, keychain misuse, insecure `UserDefaults` storage, missing jailbreak detection | P1 |
| FR-6.11 | SwiftUI/UIKit-aware code analysis: detect deprecated APIs, accessibility issues, Auto Layout constraint problems | P2 |
| FR-6.12 | Xcode project structure analysis for wiki generation: targets, schemes, build phases, dependencies | P1 |
| FR-6.13 | CoreData / SwiftData model introspection: entity relationships, migration detection | P2 |
| FR-6.14 | Graceful degradation on non-macOS platforms: Xcode tools disabled with clear messaging; Swift-only tools (SPM, swift build) available on Linux | P0 |

### 2.3 Non-Functional Requirements

| ID | Requirement | Target |
|----|-------------|--------|
| NFR-1 | CLI startup time (to first interactive prompt) | < 200ms |
| NFR-2 | Static security scan (full repo, 100k LOC) | < 30 seconds |
| NFR-3 | Wiki generation (medium project, 50k LOC) | < 10 minutes |
| NFR-4 | Headless code review (single PR diff) | < 60 seconds |
| NFR-5 | Memory usage (interactive mode, idle) | < 100 MB |
| NFR-6 | Binary size (single statically linked binary) | < 50 MB |
| NFR-7 | Platform support | Linux (amd64/arm64), macOS (arm64), Windows (amd64) |
| NFR-8 | Go version requirement | Go 1.22+ |
| NFR-9 | Zero mandatory external dependencies at runtime | All tools optional/gracefully degraded |
| NFR-10 | Test coverage for core packages | > 80% |
| NFR-11 | Skill load time (Starlark scripts) | < 50ms per skill |
| NFR-12 | Skill load time (external process) | < 200ms per skill |

---

## 3. System Architecture

### 3.1 Architecture Overview

The system follows a layered architecture with six primary layers. The Skill Runtime is a cross-cutting layer that can extend every other layer.

```
┌─────────────────────────────────────────────────────────────────────┐
│                        CLI / UI Layer                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐           │
│  │Interactive│  │ Headless │  │   Wiki   │  │ Security │           │
│  │   TUI    │  │  Runner  │  │  Runner  │  │  Audit   │           │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘           │
├───────┼──────────────┼──────────────┼──────────────┼────────────────┤
│       └──────────────┴──────┬───────┴──────────────┘                │
│                      Agent Core                                     │
│  ┌───────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │Agent Loop │  │Conversa- │  │  Tool    │  │ Context Window   │  │
│  │Plan→Act→  │  │tion Mgr  │  │ Router   │  │ Manager          │  │
│  │Observe    │  │          │  │          │  │                  │  │
│  └───────────┘  └──────────┘  └────┬─────┘  └──────────────────┘  │
├────────────────────────────────────┼───────────────────────────────┤
│                      Tool Layer    │                                │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌─────┴┐ ┌─────┐ ┌─────┐ ┌────────┐ │
│  │ File │ │Shell │ │Search│ │ Git  │ │ LSP │ │ MCP │ │ Skill  │ │
│  │ Ops  │ │ Exec │ │      │ │ Ops  │ │     │ │     │ │ Tools  │ │
│  └──────┘ └──────┘ └──────┘ └──────┘ └─────┘ └─────┘ └────────┘ │
├───────────────────────────────────────────────────────────────────┤
│                    Skill Runtime (cross-cutting)                   │
│  ┌───────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐            │
│  │  Loader   │ │ Sandbox  │ │ Registry │ │ Lifecycle│            │
│  │ & Resolver│ │ & Perms  │ │ Client   │ │ Manager  │            │
│  └───────────┘ └──────────┘ └──────────┘ └──────────┘            │
│  ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ │
│  │  Tool   │ │ Prompt  │ │ Workflow │ │ SecRule  │ │Transform │ │
│  │ Skills  │ │ Skills  │ │ Skills   │ │ Skills   │ │ Skills   │ │
│  └─────────┘ └─────────┘ └──────────┘ └──────────┘ └──────────┘ │
├───────────────────────────────────────────────────────────────────┤
│                 LLM Provider Layer                                 │
│  ┌────────────┐  ┌──────────────────┐  ┌────────┐                 │
│  │  Anthropic  │  │ OpenAI-compatible │  │ Ollama │                 │
│  └────────────┘  └──────────────────┘  └────────┘                 │
├───────────────────────────────────────────────────────────────────┤
│                 Config & Storage                                   │
│  ┌────────────┐  ┌──────────────┐  ┌──────────┐  ┌────────────┐  │
│  │Config Loader│  │Project Rules │  │  SQLite  │  │ OS Keyring │  │
│  │  (TOML)    │  │ (AGENT.md)   │  │ (History)│  │ (API Keys) │  │
│  └────────────┘  └──────────────┘  └──────────┘  └────────────┘  │
└───────────────────────────────────────────────────────────────────┘

  ┌─────────────────────────────────────────────────────────────────┐
  │              Security Analysis Engine (cross-cutting)           │
  │  ┌─────────────────────────┐  ┌──────────────────────────────┐ │
  │  │   Static Scanners       │  │   LLM-Powered Analyzers      │ │
  │  │  • Secret Scanner       │  │  • Auth/Authz Flow           │ │
  │  │  • Dependency Audit     │  │  • Data Flow / Taint         │ │
  │  │  • SAST Patterns        │  │  • Business Logic            │ │
  │  │  • Config Scanner       │  │  • Cryptography              │ │
  │  │  • License Checker      │  │  • Concurrency               │ │
  │  │  • Skill-provided rules │  │  • Skill-provided analyzers  │ │
  │  └─────────────────────────┘  └──────────────────────────────┘ │
  └─────────────────────────────────────────────────────────────────┘
```

### 3.2 Execution Modes

All three modes share the Agent Core, Tool Layer, Skill Runtime, Security Engine, and LLM Provider. Only the I/O adapter differs.

| Mode | Input | Agent Behavior | Output | Skill Activation |
|------|-------|----------------|--------|-----------------|
| Interactive | User types in TUI | Multi-turn loop with streaming display | TUI renders response + diffs | Trigger-based auto-activation + manual `/skill` command |
| Headless | `--prompt`, `--file`, stdin | Single-run loop with max-turns cap | JSON, MD, SARIF, GitHub comment | `--skills` flag or auto-detect from project config |
| Wiki | `--wiki` + codebase path | Multi-pass batch pipeline | Static site (Hugo/MkDocs/raw MD) | All project skills activated; wiki-specific skills contribute sections |

### 3.3 Agent Core

The agent core runs the **Plan → Act → Observe** loop. It is mode-agnostic. Skills integrate at multiple points in this loop.

```
┌──────────────────────────────────────────────────────────────┐
│  1. Build system prompt + conversation                        │
│     ├── Inject active Prompt Skills into system prompt        │
│     └── Inject Workflow Skill instructions (if active)        │
│  2. Send to LLM (streaming)                                   │
│  3. Parse response — text or tool_use?                        │
│  4. If tool_use → fire on_before_tool_call hooks              │
│     └── Route to Tool Layer (includes Skill-registered tools) │
│  5. Append tool result to conversation                        │
│     └── Apply Transform Skills to output (if any)             │
│  6. Fire on_after_response hooks                              │
│  7. Loop back to step 2 until done                            │
└──────────────────────────────────────────────────────────────┘
```

Key components:

- **Agent Loop:** Orchestrates the multi-turn cycle with skill hook integration at each stage.
- **Conversation Manager:** Maintains `[]Message` history. Skills can append context via lifecycle hooks.
- **Tool Router & Executor:** Dispatches tool calls. Skill-registered tools are dynamically added when a skill activates.
- **Context Window Manager:** Tracks token usage. Skills declare their context budget requirements in the manifest.
- **System Prompt Builder:** Assembles from static instructions, project rules (AGENT.md), active Prompt Skills, and skill-provided context.
- **Approval Gate:** Mode-injected callback. Skills can register approval overrides for their own tools.

### 3.4 Tool Layer

Every tool implements a common `Tool` interface. Skill-registered tools are dynamically added at activation time.

| Tool | Implementation | Source |
|------|---------------|--------|
| File Read/Write/Patch | `os` + custom diffing | Built-in |
| Shell Exec | `os/exec` | Built-in |
| Code Search | ripgrep + Go-native grep | Built-in |
| AST Search | tree-sitter | Built-in |
| LSP Client | Custom JSON-RPC | Built-in |
| Git Ops | `go-git/go-git` (pure Go) | Built-in |
| MCP Client | JSON-RPC over stdio/SSE | Built-in |
| Web Fetch | `net/http` + `goquery` | Built-in |
| Xcode Build | `xcodebuild` wrapper (macOS) | Built-in |
| Simulator | `xcrun simctl` wrapper (macOS) | Built-in |
| Swift PM | `swift package` wrapper | Built-in |
| Code Signing | `security` + `codesign` (macOS) | Built-in |
| *Skill-provided tools* | *Varies (Go, Starlark, external process)* | *Skill system* |

### 3.5 LLM Provider Layer

All providers accessed via raw `net/http` + `bufio.Scanner` for SSE parsing. No vendor SDKs (see [ADR-006](#adr-006-no-vendor-llm-sdks)).

| Provider | API Endpoint | Streaming | Auth |
|----------|-------------|-----------|------|
| Anthropic | `POST /v1/messages` (SSE) | `content_block_delta` events | `x-api-key` header |
| OpenAI-compatible | `POST /v1/chat/completions` (SSE) | `data: {"choices":[{"delta":{}}]}` | `Authorization: Bearer` |
| Ollama | `POST /api/chat` (NDJSON) | Newline-delimited JSON | None (local) |

### 3.6 Agent Skills System

See [Section 4](#4-agent-skills-system--detailed-design) for the complete skill system design.

**Summary:** Skills are pluggable units that extend the agent with new tools, domain knowledge, multi-step workflows, security rules, and output transforms. They are defined by a `SKILL.yaml` manifest and implemented via Go plugins, Starlark scripts, or external processes.

### 3.7 Security Analysis Engine

Security analysis is a standalone engine (`internal/security`) consumed by code review, wiki generator, and standalone audit. Security Rule Skills can extend it with custom scanners and rule packs.

#### Phase 1: Static Scanners (No LLM)

| Scanner | Technique | Detects |
|---------|-----------|---------|
| Secret Scanner | Regex patterns + Shannon entropy | API keys, tokens, private keys, passwords |
| Dependency Audit | OSV/NVD database queries | Known CVEs in lockfiles |
| SAST Pattern Matcher | Tree-sitter AST queries | SQL injection, path traversal, XSS, weak crypto |
| Config Scanner | File-type-specific rules | Dockerfile root, K8s privileged, CI secret leaks |
| License Checker | License file + header detection | GPL in commercial, missing licenses |
| Apple Platform Scanner | Info.plist + entitlements analysis | ATS exceptions, insecure storage, missing privacy keys |
| **Skill-provided scanners** | **Defined in Security Rule Skills** | **Custom domain-specific patterns** |

#### Phase 2: LLM-Powered Deep Analysis

| Analyzer | Focus | Example Findings |
|----------|-------|-----------------|
| Auth/Authz Flow | Authentication bypass, IDOR, privilege escalation | Missing auth middleware on admin endpoint |
| Data Flow / Taint | Untrusted input reaching dangerous sinks | HTTP param → `db.Query()` without sanitization |
| Business Logic | Logic flaws bypassing intended behavior | Negative quantity creates credit |
| Cryptography | Weak algorithms, key management | ECB mode, hardcoded key, MD5 for passwords |
| Concurrency | Race conditions, deadlocks | Concurrent map write without mutex |
| **Skill-provided analyzers** | **Defined in Security Rule Skills** | **Custom domain-specific analysis** |

#### Smart LLM Budget Prioritization

Files are scored by risk signals before LLM analysis:

| Signal | Score Weight |
|--------|-------------|
| Contains auth code (auth, login, jwt, session) | +10 |
| Contains command execution (os/exec) | +9 |
| Contains input handling (HTTP handlers, request parsing) | +8 |
| Contains database access (sql, orm, query building) | +7 |
| Contains crypto operations | +7 |
| Contains file operations (os.Open, file upload handling) | +5 |
| Already flagged by static scanners | +3 |
| Contains Keychain / security framework calls | +6 |
| Contains network (URLSession) or WebView (WKWebView) code | +5 |
| Recently modified (higher risk of new bugs) | +2 |

#### Attack Chain Correlation

After all scanners complete, the engine correlates findings into attack chains — sequences of vulnerabilities that together form complete exploit paths. Example: a missing authentication check combined with a SQL injection in the same handler chain produces a Critical-severity "Unauthenticated SQL Injection" attack chain.

### 3.8 Wiki Generator Pipeline

A 6-stage batch pipeline. Skills extend the wiki at multiple points:

```
┌──────────────┐    ┌──────────────┐    ┌──────────────────┐
│ 1. Codebase  │───▶│ 2. Chunk &   │───▶│ 3. Multi-Pass    │
│    Scanner   │    │    Classify   │    │    LLM Analysis  │
└──────────────┘    └──────────────┘    └────────┬─────────┘
     ▲ Skills can                                │
     │ add file classifiers              ┌───────▼─────────┐
                                         │ 4. Diagram Gen  │
┌──────────────┐    ┌──────────────┐    └───────┬─────────┘
│ 6. Output    │◀───│ 5. Assemble  │◀───────────┘
│              │    │  + Skill     │    Skills can contribute
│              │    │    sections  │    additional wiki sections
└──────────────┘    └──────────────┘
```

### 3.9 Config & Storage

| Concern | Tech | Details |
|---------|------|---------|
| Config format | TOML via `BurntSushi/toml` | `~/.config/aiagent/config.toml` |
| Project rules | Markdown file | `AGENT.md` at project root |
| Chat history | SQLite via `modernc.org/sqlite` | Pure Go, no CGO |
| Keyring | `zalando/go-keyring` | OS keychain integration |
| Skill state | SQLite | Skill activation, preferences, approval history |

---

## 4. Agent Skills System — Detailed Design

### 4.1 Skill Types

Skills are classified into five types based on how they extend the agent:

| Type | What It Does | Extension Point | Example |
|------|-------------|----------------|---------|
| **Tool Skill** | Registers new tools the LLM can invoke | Tool Router | Kubernetes operations, Docker management, database query, Jira integration |
| **Prompt Skill** | Injects domain knowledge and instructions into the system prompt | System Prompt Builder | DDD expert, AWS best practices, company coding standards, language-specific idioms |
| **Workflow Skill** | Defines multi-step pipelines that orchestrate tool calls and LLM reasoning | Agent Loop | RFC writer, migration planner, test generator, PR description writer |
| **Security Rule Skill** | Adds custom SAST patterns, LLM analyzers, or rule packs to the Security Engine | Security Engine | HIPAA compliance scanner, PCI-DSS rules, company-specific banned patterns |
| **Transform Skill** | Post-processes agent output before display or saving | Output Pipeline | i18n translation, tone adjustment, format conversion, redaction |

A single skill can combine multiple types. For example, a "Kubernetes Expert" skill might be both a Tool Skill (registers `kubectl` tools) and a Prompt Skill (injects K8s best practices into the system prompt).

### 4.2 Skill Manifest (SKILL.yaml)

Every skill is defined by a `SKILL.yaml` manifest in its root directory:

```yaml
# SKILL.yaml — Kubernetes Expert Skill
name: kubernetes
version: 1.2.0
description: "Kubernetes operations, troubleshooting, and best practices"
author: "community"
license: MIT
homepage: https://github.com/aiagent-skills/kubernetes

# Skill types this skill implements (can be multiple)
types:
  - tool
  - prompt
  - security-rule

# When this skill should auto-activate
triggers:
  # Activate when these files exist in the project
  files:
    - "*.yaml"
    - "Dockerfile"
    - "k8s/"
    - "helm/"
    - "kustomization.yaml"
  # Activate when these keywords appear in conversation
  keywords:
    - "kubernetes"
    - "k8s"
    - "kubectl"
    - "pod"
    - "deployment"
    - "helm"
  # Activate for specific CLI modes
  modes:
    - interactive
    - headless
    - wiki
  # Activate when these languages are detected
  languages:
    - yaml
    - dockerfile

# Permissions this skill requires (user approves on first use)
permissions:
  - shell:exec           # needs to run kubectl commands
  - file:read            # needs to read manifests
  - net:fetch             # needs to call K8s API
  - env:read              # needs KUBECONFIG

# Dependencies on other skills
dependencies:
  - name: docker          # requires the docker skill
    version: ">=1.0.0"
    optional: true        # soft dependency — works without it

# Implementation
implementation:
  # Primary: Starlark script
  backend: starlark
  entrypoint: skill.star
  # Alternative: external process
  # backend: process
  # command: ["node", "skill.js"]
  # Alternative: Go plugin
  # backend: plugin
  # path: kubernetes.so

# Prompt skill configuration
prompt:
  # Injected into system prompt when skill is active
  system_prompt_file: prompts/system.md
  # Additional context files loaded on demand
  context_files:
    - prompts/troubleshooting.md
    - prompts/best-practices.md
    - prompts/security.md
  # Maximum tokens this skill's prompt content can consume
  max_context_tokens: 4000

# Tool skill configuration
tools:
  - name: kubectl_get
    description: "Get Kubernetes resources"
    input_schema_file: schemas/kubectl_get.json
  - name: kubectl_apply
    description: "Apply a Kubernetes manifest"
    input_schema_file: schemas/kubectl_apply.json
    requires_approval: true    # always ask user before applying
  - name: kubectl_logs
    description: "Get container logs from a pod"
    input_schema_file: schemas/kubectl_logs.json

# Security rule skill configuration
security_rules:
  sast_rules_file: rules/sast.yaml
  scanners:
    - name: k8s-manifest-scanner
      entrypoint: scanners/manifests.star
  overrides_file: rules/overrides.yaml

# Wiki contribution
wiki:
  sections:
    - title: "Kubernetes Architecture"
      template: wiki/k8s-architecture.md
      analyzer: analyzers/k8s-wiki.star
  diagrams:
    - type: deployment-topology
      template: wiki/deployment-diagram.mermaid.tmpl

# Compatibility
compatibility:
  agent_version: ">=1.0.0"
  platforms:
    - linux
    - darwin
```

### 4.3 Skill Sources & Discovery

Skills are loaded from four sources in priority order (later sources override earlier):

```
Priority 1 (lowest): Built-in Skills
  Compiled into the binary. Always available. Cannot be removed.
  Examples: file-ops, shell, search, git

Priority 2: User Skills
  Location: ~/.config/aiagent/skills/
  Scope: Available in all projects for this user.
  Install: aiagent skill install <name>

Priority 3: Project Skills
  Location: .agent/skills/
  Scope: Available only in this project. Committed to git.
  Install: aiagent skill add <name>

Priority 4 (highest): Inline Skills
  Location: AGENT.md frontmatter or CLI flags
  Scope: Single session or single command.
  Usage: aiagent --skills=kubernetes,docker
```

**Discovery order within each source:**

```go
func (l *Loader) Discover(ctx context.Context) ([]SkillManifest, error) {
    var skills []SkillManifest

    // 1. Built-in (compiled into binary)
    skills = append(skills, builtinSkills...)

    // 2. User-global
    userDir := filepath.Join(configDir, "skills")
    skills = append(skills, l.scanDir(userDir)...)

    // 3. Project-local
    projectDir := filepath.Join(projectRoot, ".agent", "skills")
    skills = append(skills, l.scanDir(projectDir)...)

    // 4. AGENT.md frontmatter skills references
    skills = append(skills, l.parseAgentMDSkills()...)

    // 5. Auto-discover MCP servers as skills
    skills = append(skills, l.discoverMCPServers()...)

    return l.deduplicate(skills), nil  // later sources win
}
```

### 4.4 Skill Runtime

The Skill Runtime manages loading, validation, sandboxing, and execution of skills.

```go
type Runtime struct {
    loader      *Loader           // discovers and loads skill manifests
    registry    map[string]*Skill // active skills by name
    sandbox     *Sandbox          // permission enforcement
    lifecycle   *LifecycleManager // hook management
    starlark    *StarlarkEngine   // embedded Starlark interpreter
    processes   *ProcessManager   // external process skills
}

type Skill struct {
    Manifest    SkillManifest
    State       SkillState       // inactive, activating, active, error
    Backend     SkillBackend     // starlark, plugin, process
    Tools       []Tool           // registered tools
    Prompts     []PromptFragment // system prompt injections
    Hooks       []Hook           // lifecycle hooks
    Scanners    []StaticScanner  // security scanners
    Analyzers   []LLMAnalyzer    // security analyzers
    WikiContrib []WikiSection    // wiki contributions
}

type SkillState int
const (
    SkillInactive SkillState = iota
    SkillActivating
    SkillActive
    SkillError
)
```

**Three implementation backends:**

| Backend | Language | Performance | Isolation | Best For |
|---------|----------|-------------|-----------|----------|
| **Starlark** (embedded) | Starlark (Python-like) | Fast (in-process) | Sandboxed by default (no I/O, no net) | Simple tools, rules, transforms, prompts |
| **Go Plugin** (`.so`) | Go | Fastest (native) | Same process, full access | Performance-critical tools, complex logic |
| **External Process** | Any (Node, Python, Rust, etc.) | Slower (IPC overhead) | Process-level isolation | Existing tools, language-specific SDKs |

### 4.5 Skill Permissions & Sandboxing

Every skill declares required permissions in its manifest. The user approves permissions on first use, and the approval is stored in SQLite.

**Permission types:**

| Permission | Grants | Risk Level |
|-----------|--------|------------|
| `file:read` | Read files within project directory | Low |
| `file:write` | Create/modify/delete files | Medium |
| `shell:exec` | Execute shell commands | High |
| `net:fetch` | Make HTTP requests to external URLs | Medium |
| `llm:call` | Make additional LLM API calls (consumes tokens) | Medium |
| `git:read` | Read git history, branches, diffs | Low |
| `git:write` | Create commits, branches, tags | Medium |
| `env:read` | Read environment variables | Medium |
| `env:write` | Set environment variables for child processes | High |
| `skill:invoke` | Call other skills' functions | Low |

**Sandboxing rules:**

```go
type Sandbox struct {
    approvals  *ApprovalStore     // SQLite: user-approved permissions per skill
    policy     SandboxPolicy
}

type SandboxPolicy struct {
    // Starlark skills: restricted by default
    StarlarkAllowedModules []string  // only these Starlark modules available

    // External process skills: restricted via env and path
    ProcessAllowedPaths    []string  // filesystem paths the process can access

    // All skills: rate limits
    MaxLLMCallsPerTurn     int       // prevent runaway token usage
    MaxShellExecPerTurn    int       // prevent fork bombs
    MaxNetFetchPerTurn     int       // prevent DDoS
    ShellExecTimeout       time.Duration
    NetFetchTimeout        time.Duration
}
```

**First-use approval flow (Interactive mode):**

```
┌─────────────────────────────────────────────────────┐
│  Skill "kubernetes" wants to activate.              │
│                                                     │
│  Required permissions:                              │
│    ✓ file:read    — Read Kubernetes manifests       │
│    ✓ shell:exec   — Run kubectl commands            │
│    ✓ net:fetch    — Call Kubernetes API              │
│    ✓ env:read     — Read KUBECONFIG                 │
│                                                     │
│  [Allow once]  [Allow always]  [Deny]               │
└─────────────────────────────────────────────────────┘
```

**Headless mode:** Permissions pre-approved via config or `--approve-skills` flag.

### 4.6 Skill Lifecycle Hooks

Skills can hook into the agent loop at specific points:

```go
type Hook struct {
    Phase    HookPhase
    Handler  HookHandler
    Priority int  // lower = earlier execution
}

type HookPhase int
const (
    HookOnActivate          HookPhase = iota  // skill just activated
    HookOnDeactivate                          // skill deactivating
    HookOnConversationStart                   // new conversation begins
    HookOnBeforePromptBuild                   // before system prompt assembled
    HookOnBeforeToolCall                      // before tool executes (can intercept/modify)
    HookOnAfterToolResult                     // after tool returns result (can transform)
    HookOnAfterResponse                       // after LLM response complete
    HookOnBeforeWikiSection                   // before wiki section generated
    HookOnSecurityScanComplete                // after security scan finishes
)

type HookHandler func(ctx context.Context, event HookEvent) (*HookResult, error)

type HookEvent struct {
    Phase        HookPhase
    Skill        string
    Conversation []Message      // current conversation (read-only)
    ToolCall     *ToolUseBlock  // for before/after tool hooks
    ToolResult   *ToolResult    // for after tool hooks
    Response     string         // for after response hooks
}

type HookResult struct {
    Modified     bool           // was anything modified?
    ToolCall     *ToolUseBlock  // modified tool call (for before_tool)
    ToolResult   *ToolResult    // modified result (for after_tool)
    InjectPrompt string         // additional prompt to inject
    Cancel       bool           // cancel the tool call (for before_tool)
}
```

### 4.7 Skill Triggers & Auto-Activation

Skills declare trigger conditions. The Skill Runtime evaluates triggers continuously and activates skills when conditions are met.

```go
type TriggerContext struct {
    // File-based triggers (evaluated once at startup)
    ProjectFiles   []string         // files in project root
    DetectedLangs  []string         // languages detected by tree-sitter
    BuildSystem    string           // go.mod, package.json, Cargo.toml, etc.

    // Conversation-based triggers (evaluated each turn)
    LastUserMessage string          // latest user input
    ConversationTopic string        // LLM-classified topic

    // CLI-based triggers (evaluated once)
    Mode           string           // interactive, headless, wiki
    ExplicitSkills []string         // --skills flag
}

func (t *TriggerEngine) shouldActivate(skill *Skill, ctx TriggerContext) bool {
    triggers := skill.Manifest.Triggers

    // Explicit activation always wins
    if slices.Contains(ctx.ExplicitSkills, skill.Manifest.Name) {
        return true
    }

    // File triggers: any matching file exists
    for _, pattern := range triggers.Files {
        if matchesAny(ctx.ProjectFiles, pattern) {
            return true
        }
    }

    // Keyword triggers: keyword in user message
    for _, kw := range triggers.Keywords {
        if strings.Contains(strings.ToLower(ctx.LastUserMessage), strings.ToLower(kw)) {
            return true
        }
    }

    // Language triggers: detected language matches
    for _, lang := range triggers.Languages {
        if slices.Contains(ctx.DetectedLangs, lang) {
            return true
        }
    }

    return false
}
```

### 4.8 Skill SDK

The Skill SDK provides APIs for skill authors. There are two SDKs:

#### Go SDK (for Go plugin skills)

```go
// pkg/skillsdk/sdk.go — public API for skill authors

package skillsdk

// SkillPlugin is the interface Go plugin skills must implement
type SkillPlugin interface {
    Manifest() Manifest
    Activate(ctx Context) error
    Deactivate(ctx Context) error
}

// Context provides the skill access to agent capabilities (within permissions)
type Context interface {
    // File operations (requires file:read / file:write permission)
    ReadFile(path string) ([]byte, error)
    WriteFile(path string, content []byte) error
    ListDir(path string) ([]FileInfo, error)
    SearchFiles(pattern string) ([]string, error)

    // Shell execution (requires shell:exec permission)
    Exec(cmd string, args ...string) (ExecResult, error)
    ExecWithTimeout(timeout time.Duration, cmd string, args ...string) (ExecResult, error)

    // LLM calls (requires llm:call permission)
    Complete(prompt string, opts ...CompletionOpt) (string, error)
    CompleteJSON(prompt string, schema interface{}, opts ...CompletionOpt) (json.RawMessage, error)

    // Network (requires net:fetch permission)
    Fetch(url string, opts ...FetchOpt) (*http.Response, error)

    // Git (requires git:read / git:write permission)
    GitDiff(base, head string) (string, error)
    GitLog(n int) ([]GitCommit, error)
    GitStatus() ([]GitFileStatus, error)

    // Environment (requires env:read permission)
    Env(key string) string

    // Skill-to-skill communication (requires skill:invoke permission)
    InvokeSkill(name, function string, args map[string]interface{}) (interface{}, error)

    // Project metadata (always available)
    ProjectRoot() string
    ProjectLanguage() string
    ProjectBuildSystem() string

    // Logging
    Log(level LogLevel, msg string, args ...interface{})
}
```

#### Starlark SDK (for script skills)

```starlark
# Available to all Starlark skills via built-in functions

# File operations (requires file:read / file:write)
content = read_file("path/to/file.go")
write_file("path/to/output.md", content)
files = list_dir("internal/")
matches = search_files("*.go")

# Shell execution (requires shell:exec)
result = exec("kubectl", "get", "pods", "-n", namespace)
# result.stdout, result.stderr, result.exit_code

# LLM calls (requires llm:call)
answer = llm_complete("Summarize this code: " + content)
structured = llm_json("Extract functions from this code: " + content, schema={
    "functions": [{"name": "string", "params": ["string"]}]
})

# Network (requires net:fetch)
response = fetch("https://api.example.com/data")
# response.status, response.body, response.json()

# Git (requires git:read)
diff = git_diff("HEAD~1", "HEAD")
log = git_log(10)

# Environment (requires env:read)
kubeconfig = env("KUBECONFIG")

# Project metadata (always available)
root = project_root()
lang = project_language()

# Register a tool for the LLM to call
def handle_kubectl_get(input):
    """Execute kubectl get command."""
    resource = input["resource"]
    namespace = input.get("namespace", "default")
    result = exec("kubectl", "get", resource, "-n", namespace, "-o", "json")
    return result.stdout

register_tool(
    name = "kubectl_get",
    description = "Get Kubernetes resources",
    input_schema = {
        "type": "object",
        "properties": {
            "resource": {"type": "string", "description": "Resource type (pods, services, etc.)"},
            "namespace": {"type": "string", "description": "Kubernetes namespace", "default": "default"},
        },
        "required": ["resource"]
    },
    handler = handle_kubectl_get,
    requires_approval = False,
)

# Register a lifecycle hook
def on_activate(ctx):
    result = exec("kubectl", "version", "--client")
    if result.exit_code != 0:
        log("warn", "kubectl not found in PATH")

register_hook("on_activate", on_activate)
```

### 4.9 Skill Composition & Dependencies

Skills can declare dependencies on other skills:

```yaml
dependencies:
  - name: docker
    version: ">=1.0.0"
    optional: false    # hard dependency
  - name: helm
    version: ">=2.0.0"
    optional: true     # soft dependency
```

**Resolution order:**

1. Build dependency graph from all discovered skills.
2. Activate in topological order (dependencies first).
3. Circular dependencies detected and reported as errors.
4. Optional dependencies activated if available, skipped if not.

**Skill-to-skill communication:**

```starlark
# In the kubernetes skill, call the docker skill
docker_info = invoke_skill("docker", "get_container_info", {
    "container": pod_container_id
})
```

### 4.10 Built-in Skills

These skills are compiled into the binary and always available:

| Skill Name | Type | Description |
|-----------|------|-------------|
| `core-tools` | Tool | File read/write/patch, shell exec, code search — the base toolset |
| `git` | Tool + Prompt | Git operations + git workflow best practices |
| `code-review` | Workflow + Prompt | Structured code review with findings categories |
| `log-analysis` | Workflow + Prompt | SRE log analysis with root cause template |
| `security-base` | Security Rule | OWASP Top 10 SAST rules, secret patterns, dependency audit |
| `apple-dev` | Tool + Prompt + Security Rule | Xcode build/test/archive, simctl, SPM, signing, iOS security rules |
| `wiki-base` | Workflow | Base wiki generation pipeline with standard sections |

### 4.11 Example Skills

#### Example: Tool Skill — Database Query

```
.agent/skills/database/
├── SKILL.yaml
├── skill.star
└── schemas/
    └── query.json
```

```yaml
# SKILL.yaml
name: database
version: 1.0.0
description: "Query databases to understand data models and debug issues"
types: [tool]
triggers:
  files: ["*.sql", "migrations/", "schema.prisma", "sqlc.yaml"]
  keywords: ["database", "sql", "query", "table", "migration"]
permissions: [shell:exec, env:read]
implementation:
  backend: starlark
  entrypoint: skill.star
tools:
  - name: db_query
    description: "Execute a read-only SQL query against the project database"
    input_schema_file: schemas/query.json
    requires_approval: true
  - name: db_schema
    description: "Show database schema (tables, columns, types)"
    input_schema_file: schemas/schema.json
```

```starlark
# skill.star
def handle_db_query(input):
    query = input["query"]
    if not query.strip().upper().startswith("SELECT"):
        return error("Only SELECT queries are allowed for safety")
    db_url = env("DATABASE_URL")
    if not db_url:
        return error("DATABASE_URL not set")
    result = exec("psql", db_url, "-c", query, "--csv")
    return result.stdout

def handle_db_schema(input):
    db_url = env("DATABASE_URL")
    result = exec("psql", db_url, "-c",
        "SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name, ordinal_position",
        "--csv")
    return result.stdout

register_tool("db_query", handle_db_query, requires_approval=True)
register_tool("db_schema", handle_db_schema)
```

#### Example: Prompt Skill — Domain-Driven Design Expert

```yaml
# SKILL.yaml
name: ddd-expert
version: 1.0.0
description: "Domain-Driven Design expertise for modeling and architecture decisions"
types: [prompt]
triggers:
  keywords: ["domain", "bounded context", "aggregate", "DDD", "event sourcing", "CQRS"]
permissions: []   # prompt skills typically need no permissions
prompt:
  system_prompt_file: prompts/system.md
  context_files:
    - prompts/bounded-contexts.md
    - prompts/aggregates.md
  max_context_tokens: 3000
```

```markdown
<!-- prompts/system.md -->
You are an expert in Domain-Driven Design (DDD). When helping with architecture
and modeling decisions, apply these principles:

- Identify bounded contexts by analyzing business capabilities and team boundaries
- Design aggregates around invariants that must be enforced transactionally
- Prefer events for cross-context communication (eventual consistency)
- Use ubiquitous language: the code should read like the domain expert speaks
- Apply the repository pattern for aggregate persistence
- Value objects for concepts with no identity; entities for concepts with identity
- Anti-corruption layers at context boundaries to prevent model leakage
```

#### Example: Workflow Skill — RFC Writer

```yaml
# SKILL.yaml
name: rfc-writer
version: 1.0.0
description: "Generate structured RFC/design documents from a problem statement"
types: [workflow]
triggers:
  keywords: ["RFC", "design doc", "proposal", "technical spec"]
permissions: [file:read, file:write, llm:call, git:read]
implementation:
  backend: starlark
  entrypoint: skill.star
```

```starlark
# skill.star
def run_rfc_workflow(input):
    topic = input["topic"]

    # Step 1: Gather codebase context
    log("info", "Gathering codebase context...")
    relevant_files = search_files("*" + topic.replace(" ", "*") + "*")
    context = ""
    for f in relevant_files[:10]:
        context += "--- " + f + " ---\n" + read_file(f) + "\n\n"

    # Step 2: Generate RFC outline
    log("info", "Generating RFC outline...")
    outline = llm_complete(
        "Based on this codebase context, generate a detailed RFC outline for: " + topic +
        "\n\nCodebase context:\n" + context +
        "\n\nUse this template:\n" + load_file("prompts/rfc-template.md")
    )

    # Step 3: Expand each section
    log("info", "Expanding sections...")
    git_history = git_log(20)
    full_rfc = llm_complete(
        "Expand this RFC outline into a complete document. Include code examples, " +
        "architecture diagrams (Mermaid), and reference git history for context.\n\n" +
        "Outline:\n" + outline +
        "\n\nRecent git history:\n" + str(git_history)
    )

    # Step 4: Write the RFC
    filename = "docs/rfcs/rfc-" + topic.lower().replace(" ", "-") + ".md"
    write_file(filename, full_rfc)
    return "RFC generated at " + filename

register_workflow("write-rfc", run_rfc_workflow)
```

#### Example: Security Rule Skill — HIPAA Compliance

```yaml
# SKILL.yaml
name: hipaa-compliance
version: 1.0.0
description: "HIPAA compliance rules for healthcare applications"
types: [security-rule]
triggers:
  files: [".hipaa", "compliance/", "phi/"]
  keywords: ["HIPAA", "PHI", "healthcare", "patient data"]
permissions: [file:read]
security_rules:
  sast_rules_file: rules/sast.yaml
  scanners:
    - name: phi-detector
      entrypoint: scanners/phi-detector.star
```

```yaml
# rules/sast.yaml
rules:
  - id: HIPAA-LOG-001
    title: "PHI logged without redaction"
    severity: critical
    category: data-exposure
    cwe: CWE-532
    pattern:
      type: regex
      match: 'log\.(Print|Info|Debug|Warn|Error).*\b(ssn|patient|diagnosis|medication|dob)\b'
      exclude_paths: ["*_test.go", "test/"]
    remediation: "Use the PHI redaction helper before logging patient data"

  - id: HIPAA-ENCRYPT-001
    title: "PHI stored without encryption at rest"
    severity: high
    category: cryptography
    cwe: CWE-311
    pattern:
      type: tree-sitter
      query: |
        (call_expression
          function: (selector_expression field: (field_identifier) @method)
          (#match? @method "^(Insert|Create|Save|Put|Set)$"))
    remediation: "Wrap PHI data with crypto.EncryptPHI() before database storage"
```

#### Example: Transform Skill — i18n Output

```yaml
# SKILL.yaml
name: i18n-output
version: 1.0.0
description: "Translate agent output to the user's preferred language"
types: [transform]
triggers:
  modes: [interactive]
permissions: [llm:call]
implementation:
  backend: starlark
  entrypoint: skill.star
```

```starlark
# skill.star
def transform_output(ctx):
    target_lang = ctx.config.get("language", "en")
    if target_lang == "en":
        return hook_result()
    translated = llm_complete(
        "Translate the following to " + target_lang + ". " +
        "Preserve all code blocks, file paths, and technical terms unchanged.\n\n" +
        ctx.response
    )
    return hook_result(modified_response=translated)

register_hook("on_after_response", transform_output)
```

#### Example: Built-in Tool + Prompt + Security Rule Skill — Apple Development

The `apple-dev` skill is a comprehensive built-in skill that ships with the agent binary. It demonstrates a multi-type skill combining tools, prompt injection, and security rules.

```
internal/skills/builtin/apple_dev/
├── SKILL.yaml
├── tools/
│   ├── xcodebuild.go       # Build, test, archive, clean
│   ├── simctl.go            # Simulator management
│   ├── spm.go               # Swift Package Manager
│   ├── codesign.go          # Signing introspection
│   └── xcrun.go             # Generic xcrun dispatch
├── prompts/
│   ├── system.md            # Apple platform best practices
│   ├── swiftui.md           # SwiftUI patterns and conventions
│   ├── uikit.md             # UIKit patterns
│   ├── concurrency.md       # Swift concurrency (async/await, actors)
│   └── signing.md           # Code signing troubleshooting guide
├── rules/
│   ├── ios-sast.yaml        # iOS-specific SAST rules
│   └── plist-rules.yaml     # Info.plist security rules
├── schemas/
│   ├── xcodebuild.json
│   ├── simctl.json
│   ├── spm.json
│   └── codesign.json
└── wiki/
    └── xcode-project.md.tmpl  # Wiki template for Xcode projects
```

```yaml
# SKILL.yaml — Apple Development (built-in)
name: apple-dev
version: 1.0.0
description: "Xcode CLI tools, Swift/iOS best practices, and Apple platform security scanning"
author: "built-in"
types:
  - tool
  - prompt
  - security-rule

triggers:
  files:
    - "*.xcodeproj"
    - "*.xcworkspace"
    - "Package.swift"
    - "Podfile"
    - "*.swift"
    - "Info.plist"
    - "*.entitlements"
    - "*.storyboard"
    - "*.xib"
    - "*.xcdatamodeld"
    - "Cartfile"
    - "*.pbxproj"
  languages:
    - swift
    - objective-c
  keywords:
    - "xcode"
    - "xcodebuild"
    - "simulator"
    - "swift"
    - "ios"
    - "macos"
    - "swiftui"
    - "uikit"
    - "cocoapods"
    - "spm"

permissions:
  - shell:exec       # run xcodebuild, xcrun, swift, simctl
  - file:read        # read project files, plists, entitlements
  - env:read         # read DEVELOPER_DIR, CODE_SIGN_IDENTITY

prompt:
  system_prompt_file: prompts/system.md
  context_files:
    - prompts/swiftui.md
    - prompts/concurrency.md
    - prompts/signing.md
  max_context_tokens: 5000

tools:
  - name: xcode_build
    description: "Build an Xcode project or workspace. Parses output for errors and warnings."
    input_schema_file: schemas/xcodebuild.json
  - name: xcode_test
    description: "Run tests for an Xcode project. Returns structured test results with pass/fail/skip counts."
    input_schema_file: schemas/xcodebuild.json
  - name: xcode_archive
    description: "Create an archive for distribution. Requires code signing."
    input_schema_file: schemas/xcodebuild.json
    requires_approval: true
  - name: xcode_clean
    description: "Clean build artifacts for a scheme"
    input_schema_file: schemas/xcodebuild.json
  - name: sim_list
    description: "List available iOS/watchOS/tvOS simulators with their states"
    input_schema_file: schemas/simctl.json
  - name: sim_boot
    description: "Boot a simulator device by name or UDID"
    input_schema_file: schemas/simctl.json
  - name: sim_install
    description: "Install an app bundle on a booted simulator"
    input_schema_file: schemas/simctl.json
  - name: sim_launch
    description: "Launch an app on a booted simulator"
    input_schema_file: schemas/simctl.json
  - name: sim_screenshot
    description: "Capture a screenshot from a booted simulator"
    input_schema_file: schemas/simctl.json
  - name: sim_log
    description: "Stream or filter device/simulator logs via `xcrun simctl spawn log`"
    input_schema_file: schemas/simctl.json
  - name: swift_build
    description: "Build a Swift package (works on macOS and Linux)"
    input_schema_file: schemas/spm.json
  - name: swift_test
    description: "Run Swift package tests with structured output"
    input_schema_file: schemas/spm.json
  - name: swift_resolve
    description: "Resolve Swift package dependencies"
    input_schema_file: schemas/spm.json
  - name: swift_add_dep
    description: "Add a Swift package dependency to Package.swift"
    input_schema_file: schemas/spm.json
    requires_approval: true
  - name: codesign_info
    description: "Show code signing identities, provisioning profiles, and entitlements"
    input_schema_file: schemas/codesign.json
  - name: codesign_verify
    description: "Verify code signature of a built app bundle"
    input_schema_file: schemas/codesign.json

security_rules:
  sast_rules_file: rules/ios-sast.yaml
  scanners:
    - name: plist-scanner
      entrypoint: rules/plist-scanner.go

wiki:
  sections:
    - title: "Xcode Project Structure"
      template: wiki/xcode-project.md.tmpl
  diagrams:
    - type: target-dependency-graph
      template: wiki/target-deps.mermaid.tmpl

compatibility:
  agent_version: ">=1.0.0"
  platforms:
    - darwin    # Full Xcode tools (xcodebuild, simctl, codesign)
    # On linux: only swift_build, swift_test, swift_resolve available
```

**Apple platform system prompt (injected when skill is active):**

```markdown
<!-- prompts/system.md -->
You are an expert Apple platform developer. When assisting with iOS, macOS, watchOS,
or tvOS projects, follow these principles:

## Build System
- Prefer `xcodebuild` with `-quiet` for cleaner output; parse structured build logs
- Always specify `-scheme`, `-destination`, and `-configuration` explicitly
- For SPM-only projects, prefer `swift build` / `swift test` over xcodebuild
- Use `-resultBundlePath` for machine-readable test results

## Swift Best Practices
- Use Swift concurrency (async/await, actors) over GCD for new code
- Prefer value types (structs, enums) over classes when no reference semantics needed
- Use @Observable (iOS 17+) over ObservableObject for new SwiftUI code
- Apply access control: default to private, expose only what's needed
- Use Swift's strong type system: avoid Any, force-unwraps, and stringly-typed APIs

## SwiftUI
- Extract complex views into smaller components
- Use @State for view-local state, @Binding for parent-owned state
- Prefer .task {} over .onAppear {} for async work
- Use #Preview macro for Xcode 15+ previews

## Security
- Never store secrets in UserDefaults — use Keychain Services
- Enable App Transport Security (ATS) — justify any exceptions
- Use CryptoKit for cryptographic operations, not CommonCrypto
- Implement certificate pinning for sensitive API communication
- Add jailbreak/integrity detection for sensitive apps

## Code Signing
- When troubleshooting signing, check: identity, profile, entitlements, team ID
- Use automatic signing for development, manual for CI/CD and distribution
- Always verify entitlements match between profile and target
```

**iOS-specific SAST rules:**

```yaml
# rules/ios-sast.yaml
rules:
  - id: IOS-SEC-001
    title: "Sensitive data stored in UserDefaults"
    severity: high
    category: data-exposure
    cwe: CWE-922
    languages: [swift, objective-c]
    pattern:
      type: tree-sitter
      query: |
        (call_expression
          (member_expression
            object: (identifier) @obj
            property: (property_identifier) @method)
          (#eq? @obj "UserDefaults")
          (#match? @method "^(set|setValue)"))
    context_check: "Verify no passwords, tokens, PII, or keys stored in UserDefaults"
    remediation: "Use Keychain Services via Security.framework for sensitive data"

  - id: IOS-SEC-002
    title: "App Transport Security exception allows insecure HTTP"
    severity: high
    category: misconfiguration
    cwe: CWE-319
    languages: [xml]
    pattern:
      type: regex
      match: '<key>NSAllowsArbitraryLoads</key>\s*<true/>'
      include_paths: ["*/Info.plist"]
    remediation: "Remove NSAllowsArbitraryLoads or restrict to specific domains via NSExceptionDomains"

  - id: IOS-SEC-003
    title: "Hardcoded API key in Swift source"
    severity: critical
    category: secrets-exposure
    cwe: CWE-798
    languages: [swift]
    pattern:
      type: regex
      match: '(let|var)\s+\w*(api|API|Api)(Key|key|Secret|secret|Token|token)\w*\s*[:=]\s*"[A-Za-z0-9_\-]{16,}"'
      exclude_paths: ["*Tests*", "*Mock*", "*.example*"]
    remediation: "Store API keys in Keychain, .xcconfig (gitignored), or a secrets manager"

  - id: IOS-SEC-004
    title: "WKWebView allows arbitrary JavaScript execution"
    severity: medium
    category: injection
    cwe: CWE-79
    languages: [swift]
    pattern:
      type: tree-sitter
      query: |
        (call_expression
          (member_expression
            property: (property_identifier) @method)
          (#eq? @method "evaluateJavaScript"))
    context_check: "Verify JavaScript input is not derived from untrusted sources"
    remediation: "Sanitize JavaScript strings or use WKScriptMessageHandler for safe communication"

  - id: IOS-SEC-005
    title: "Using deprecated CommonCrypto instead of CryptoKit"
    severity: medium
    category: cryptography
    cwe: CWE-327
    languages: [swift]
    pattern:
      type: regex
      match: 'import\s+CommonCrypto|CCCrypt|CC_SHA1|CC_MD5'
    remediation: "Use CryptoKit (iOS 13+) for modern, safe cryptographic operations"

  - id: IOS-SEC-006
    title: "Missing jailbreak detection in security-sensitive context"
    severity: medium
    category: authentication
    languages: [swift]
    pattern:
      type: llm-hint
      description: "Check if app handles financial data or auth tokens but lacks jailbreak detection"
    remediation: "Implement runtime integrity checks for security-sensitive applications"

  - id: IOS-SEC-007
    title: "Insecure clipboard usage with sensitive data"
    severity: medium
    category: data-exposure
    cwe: CWE-200
    languages: [swift]
    pattern:
      type: tree-sitter
      query: |
        (member_expression
          object: (identifier) @obj
          property: (property_identifier) @prop
          (#eq? @obj "UIPasteboard")
          (#eq? @prop "general"))
    context_check: "Verify no sensitive data (passwords, tokens) is written to the general pasteboard"
    remediation: "Use local-only pasteboard or clear sensitive data immediately after use"

  - id: IOS-SEC-008
    title: "Biometric auth without device passcode fallback policy"
    severity: medium
    category: authentication
    cwe: CWE-287
    languages: [swift]
    pattern:
      type: regex
      match: 'LAPolicy\.deviceOwnerAuthenticationWithBiometrics(?!OrDevicePasscode)'
    remediation: "Use .deviceOwnerAuthentication to allow passcode fallback when biometrics fail"

  - id: IOS-SEC-009
    title: "Logging potentially sensitive data with os_log or NSLog"
    severity: medium
    category: data-exposure
    cwe: CWE-532
    languages: [swift, objective-c]
    pattern:
      type: regex
      match: '(os_log|NSLog|print)\(.*\b(token|password|secret|ssn|credit.?card)\b'
      exclude_paths: ["*Tests*"]
    remediation: "Redact sensitive fields before logging; use OSLog with .private visibility"

  - id: IOS-SEC-010
    title: "URL scheme handler without input validation"
    severity: high
    category: input-validation
    cwe: CWE-20
    languages: [swift]
    pattern:
      type: tree-sitter
      query: |
        (function_declaration
          name: (identifier) @name
          (#match? @name "^application.*open.*url"))
    context_check: "Verify URL scheme handler validates source app and sanitizes URL parameters"
    remediation: "Validate the sourceApplication and sanitize all URL parameters before processing"
```

**Core xcodebuild tool implementation (Go, built-in):**

```go
// internal/tools/xcode/xcodebuild.go

package xcode

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "runtime"
    "strings"
)

type XcodeBuildTool struct {
    action string // build, test, archive, clean
}

type XcodeBuildInput struct {
    Workspace     string `json:"workspace,omitempty"`
    Project       string `json:"project,omitempty"`
    Scheme        string `json:"scheme"`
    Destination   string `json:"destination,omitempty"`
    Configuration string `json:"configuration,omitempty"`
    ExtraArgs     []string `json:"extra_args,omitempty"`
}

func (t *XcodeBuildTool) Name() string {
    return "xcode_" + t.action
}

func (t *XcodeBuildTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
    if runtime.GOOS != "darwin" {
        return ToolResult{
            Content: "xcodebuild is only available on macOS. Use swift_build/swift_test for SPM projects on Linux.",
            IsError: true,
        }, nil
    }

    var in XcodeBuildInput
    if err := json.Unmarshal(input, &in); err != nil {
        return ToolResult{}, fmt.Errorf("invalid input: %w", err)
    }

    args := []string{t.action}

    if in.Workspace != "" {
        args = append(args, "-workspace", in.Workspace)
    } else if in.Project != "" {
        args = append(args, "-project", in.Project)
    }

    args = append(args, "-scheme", in.Scheme)

    if in.Destination != "" {
        args = append(args, "-destination", in.Destination)
    } else {
        // Default: latest iOS simulator
        args = append(args, "-destination", "platform=iOS Simulator,name=iPhone 16")
    }

    if in.Configuration != "" {
        args = append(args, "-configuration", in.Configuration)
    }

    args = append(args, "-quiet")             // less noise
    args = append(args, "-resultBundlePath", "/tmp/xcode-result")  // structured results
    args = append(args, in.ExtraArgs...)

    cmd := exec.CommandContext(ctx, "xcodebuild", args...)
    output, err := cmd.CombinedOutput()

    result := parseBuildOutput(string(output), err)
    return ToolResult{Content: result}, nil
}

func parseBuildOutput(output string, execErr error) string {
    var sb strings.Builder

    errors, warnings := extractDiagnostics(output)

    if execErr != nil {
        sb.WriteString("❌ BUILD FAILED\n\n")
    } else {
        sb.WriteString("✅ BUILD SUCCEEDED\n\n")
    }

    if len(errors) > 0 {
        sb.WriteString(fmt.Sprintf("Errors (%d):\n", len(errors)))
        for _, e := range errors {
            sb.WriteString("  • " + e + "\n")
        }
        sb.WriteString("\n")
    }

    if len(warnings) > 0 {
        sb.WriteString(fmt.Sprintf("Warnings (%d):\n", len(warnings)))
        for _, w := range warnings {
            sb.WriteString("  • " + w + "\n")
        }
    }

    // Include raw output tail for context
    lines := strings.Split(output, "\n")
    if len(lines) > 30 {
        lines = lines[len(lines)-30:]
    }
    sb.WriteString("\n--- Last 30 lines ---\n")
    sb.WriteString(strings.Join(lines, "\n"))

    return sb.String()
}

// Available returns true only on macOS with Xcode installed
func Available() bool {
    if runtime.GOOS != "darwin" {
        return false
    }
    _, err := exec.LookPath("xcodebuild")
    return err == nil
}
```

**Simulator management tool:**

```go
// internal/tools/xcode/simctl.go

package xcode

type SimctlTool struct {
    action string // list, boot, shutdown, install, launch, screenshot, log
}

type SimctlInput struct {
    DeviceName string `json:"device_name,omitempty"`
    DeviceUDID string `json:"device_udid,omitempty"`
    AppPath    string `json:"app_path,omitempty"`
    BundleID   string `json:"bundle_id,omitempty"`
    OutputPath string `json:"output_path,omitempty"`
    Predicate  string `json:"predicate,omitempty"` // for log filtering
}

func (t *SimctlTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
    if runtime.GOOS != "darwin" {
        return ToolResult{Content: "Simulator is only available on macOS.", IsError: true}, nil
    }

    var in SimctlInput
    json.Unmarshal(input, &in)

    switch t.action {
    case "list":
        return t.list(ctx)
    case "boot":
        return t.boot(ctx, in.deviceID())
    case "shutdown":
        return t.shutdown(ctx, in.deviceID())
    case "install":
        return t.install(ctx, in.deviceID(), in.AppPath)
    case "launch":
        return t.launch(ctx, in.deviceID(), in.BundleID)
    case "screenshot":
        return t.screenshot(ctx, in.deviceID(), in.OutputPath)
    case "log":
        return t.streamLog(ctx, in.deviceID(), in.Predicate)
    }
    return ToolResult{IsError: true, Content: "unknown simctl action"}, nil
}

func (t *SimctlTool) list(ctx context.Context) (ToolResult, error) {
    cmd := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "--json")
    output, err := cmd.Output()
    if err != nil {
        return ToolResult{Content: "Failed to list simulators: " + err.Error(), IsError: true}, nil
    }

    // Parse JSON and return a human-readable summary
    summary := formatSimulatorList(output)
    return ToolResult{Content: summary}, nil
}
```

### 4.12 Skill Registry & Distribution

Skills are distributed via a git-based registry:

```bash
# Install from registry
aiagent skill install kubernetes
aiagent skill install hipaa-compliance@1.2.0

# Install from git URL
aiagent skill install github.com/company/custom-skill

# Install from local path
aiagent skill install ./my-skill

# Search registry
aiagent skill search "kubernetes"

# List installed
aiagent skill list

# Create from template
aiagent skill create my-new-skill --type=tool
```

**Registry structure (git repo):**

```
registry/
├── index.yaml              # searchable index of all skills
├── skills/
│   ├── kubernetes/
│   │   ├── metadata.yaml   # name, description, author, downloads
│   │   └── versions/
│   │       ├── 1.0.0.yaml  # git URL + SHA for this version
│   │       └── 1.2.0.yaml
│   ├── docker/
│   └── ...
```

The registry is a YAML index in a public git repo. No server required — the CLI clones/fetches it.

---

## 5. Project Structure

```
aiagent/
├── cmd/agent/
│   ├── main.go                 # Root command, flag parsing, mode dispatch
│   ├── interactive.go          # Launches Bubble Tea TUI
│   ├── headless.go             # Launches headless runner
│   ├── wiki.go                 # Launches wiki pipeline
│   └── skill.go                # Skill management subcommands
├── internal/
│   ├── agent/                  # Agent core (shared across all modes)
│   │   ├── loop.go             # Plan → Act → Observe agentic loop
│   │   ├── conversation.go     # Message history management
│   │   └── context.go          # Token tracking and truncation
│   ├── provider/               # LLM provider abstraction
│   │   ├── interface.go        # LLMProvider interface + StreamEvent
│   │   ├── anthropic/          # Anthropic Messages API (SSE)
│   │   ├── openai/             # OpenAI-compatible Chat Completions
│   │   └── ollama/             # Local Ollama API
│   ├── tools/                  # Tool interface + built-in implementations
│   │   ├── interface.go        # Tool interface definition
│   │   ├── file.go, shell.go   # Core tools
│   │   ├── search.go, git.go   # Search + git
│   │   ├── lsp.go, mcp.go     # Integration tools
│   │   ├── web.go              # Web fetch
│   │   └── xcode/              # Apple platform tools (macOS)
│   │       ├── xcodebuild.go   # Build, test, archive
│   │       ├── simctl.go       # Simulator management
│   │       ├── spm.go          # Swift Package Manager
│   │       ├── codesign.go     # Signing & provisioning
│   │       ├── xcrun.go        # Generic xcrun dispatch
│   │       └── parser.go       # Build log & test result parsing
│   ├── skills/                 # Skill Runtime
│   │   ├── runtime.go          # Skill Runtime orchestrator
│   │   ├── loader.go           # Skill discovery and manifest parsing
│   │   ├── manifest.go         # SKILL.yaml schema and validation
│   │   ├── triggers.go         # Trigger evaluation engine
│   │   ├── hooks.go            # Lifecycle hook management
│   │   ├── sandbox.go          # Permission enforcement
│   │   ├── starlark.go         # Embedded Starlark engine
│   │   ├── process.go          # External process skill management
│   │   ├── plugin.go           # Go plugin loader
│   │   ├── registry.go         # Remote registry client
│   │   └── builtin/            # Built-in skills (compiled in)
│   │       ├── core_tools.go
│   │       ├── git.go
│   │       ├── code_review.go
│   │       ├── log_analysis.go
│   │       ├── security_base.go
│   │       └── wiki_base.go
│   ├── security/               # Security Analysis Engine
│   │   ├── engine.go           # Orchestrator
│   │   ├── finding.go          # Finding, Severity, Category types
│   │   ├── correlator.go       # Attack chain detection
│   │   ├── prioritizer.go      # LLM budget allocation
│   │   ├── scanner/            # Static scanners
│   │   │   ├── secrets.go, deps.go, sast.go
│   │   │   ├── config.go, license.go
│   │   │   ├── apple.go          # Info.plist, entitlements, ATS scanner
│   │   │   └── skill_scanner.go  # Adapter for Skill-provided scanners
│   │   ├── analyzer/           # LLM-powered analyzers
│   │   │   ├── auth.go, dataflow.go, business.go
│   │   │   ├── crypto.go, concurrency.go
│   │   │   └── skill_analyzer.go # Adapter for Skill-provided analyzers
│   │   ├── rules/              # owasp.go, cwe.go, custom.go
│   │   └── output/             # sarif, md, github_pr, json, wiki, cyclonedx
│   ├── tui/                    # Bubble Tea interactive UI
│   ├── runner/headless.go      # Headless execution runner
│   ├── pipeline/               # codereview.go, loganalysis.go
│   ├── wiki/                   # Wiki generator pipeline
│   │   ├── pipeline.go, scanner.go, chunker.go
│   │   ├── analyzer.go, diagrams.go
│   │   ├── assembler.go        # Merges skill-contributed wiki sections
│   │   └── renderer.go
│   ├── config/
│   └── store/                  # SQLite: conversations + skill approvals
├── pkg/
│   └── skillsdk/               # Public SDK for Go plugin skill authors
│       ├── sdk.go              # Context interface + helpers
│       ├── manifest.go         # Manifest types
│       └── testing.go          # Test helpers for skill authors
├── .agent/skills/              # Project-local skills (git-committed)
├── .security.yaml
├── AGENT.md
└── go.mod
```

---

## 6. Key Interface Definitions

### LLMProvider Interface

```go
type LLMProvider interface {
    Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}

type CompletionRequest struct {
    Model       string
    System      string
    Messages    []Message
    Tools       []ToolDef    // includes skill-registered tools
    MaxTokens   int
    Temperature float64
}

type StreamEvent struct {
    Type    string          // "text_delta", "tool_use", "stop", "error"
    Text    string
    ToolUse *ToolUseBlock
    Error   error
}
```

### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

// SkillTool wraps a skill-provided tool with permission checks
type SkillTool struct {
    skill     string
    tool      Tool
    sandbox   *Sandbox
    approval  bool
}
```

### Skill Runtime Interface

```go
type SkillRuntime interface {
    Discover(ctx context.Context) ([]SkillManifest, error)
    Load(ctx context.Context, name string) (*Skill, error)
    Activate(ctx context.Context, name string) error
    Deactivate(ctx context.Context, name string) error
    ActiveSkills() []*Skill
    EvaluateTriggers(ctx context.Context, triggerCtx TriggerContext) []string
    RegisteredTools() []Tool
    SystemPromptFragments() []PromptFragment
    FireHook(ctx context.Context, phase HookPhase, event HookEvent) (*HookResult, error)
    SecurityScanners() []StaticScanner
    SecurityAnalyzers() []LLMAnalyzer
    WikiSections() []WikiSection
}
```

### Security Scanner Interfaces

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
```

### Approval Function (Mode Injection)

```go
type ApprovalFunc func(ctx context.Context, action ToolAction) (bool, error)

// Headless mode: auto-approve skills listed in config
func headlessSkillApproval(config HeadlessConfig) ApprovalFunc {
    return func(ctx context.Context, action ToolAction) (bool, error) {
        if slices.Contains(config.ApprovedSkills, action.SkillName) {
            return slices.Contains(config.AllowedTools, action.ToolName), nil
        }
        return false, nil
    }
}
```

---

## 7. Technology Reference

### 7.1 Core Go Libraries

| Library | Purpose | License | Justification |
|---------|---------|---------|---------------|
| `charmbracelet/bubbletea` | TUI framework (Elm architecture) | MIT | De facto standard for Go TUI |
| `charmbracelet/lipgloss` | Terminal styling | MIT | Companion to Bubble Tea |
| `charmbracelet/glamour` | Markdown rendering in terminal | MIT | Best-in-class terminal markdown |
| `charmbracelet/bubbles` | TUI components | MIT | Pre-built components |
| `spf13/cobra` | CLI framework | Apache 2.0 | Subcommands, flags, completions |
| `smacker/go-tree-sitter` | Multi-language AST parsing | MIT | 50+ languages, SAST + wiki scanner |
| `go-git/go-git` | Pure Go git | Apache 2.0 | No C dependency |
| `modernc.org/sqlite` | SQLite (pure Go, no CGO) | BSD | Conversations + skill state |
| `BurntSushi/toml` | TOML config parsing | MIT | Config files |
| `pkoukk/tiktoken-go` | Token counting | MIT | Context window management |
| `sourcegraph/conc` | Structured concurrency | MIT | Parallel LLM calls |
| `golang.org/x/time/rate` | Rate limiting | BSD-3 | Throttle API calls |
| `zalando/go-keyring` | OS keychain | MIT | Secure API key storage |
| `invopop/jsonschema` | JSON Schema from Go structs | MIT | Tool input schemas |
| `stretchr/testify` | Testing | MIT | Assertions + mocks |

### 7.2 Security-Specific Libraries

| Library | Purpose | License |
|---------|---------|---------|
| `google/osv-scanner` | OSV.dev vulnerability database | Apache 2.0 |
| `golang.org/x/vuln` (govulncheck) | Go-specific vulnerability checking | BSD-3 |
| `owenrumney/go-sarif` | SARIF output | MIT |
| `CycloneDX/cyclonedx-go` | CycloneDX SBOM | Apache 2.0 |
| `google/go-github` | GitHub API client | BSD-3 |

### 7.3 Skills-Specific Libraries

| Library | Purpose | License | Justification |
|---------|---------|---------|---------------|
| `go.starlark.net` | Starlark interpreter (embedded scripting) | BSD-3 | See [ADR-008](#adr-008-starlark-as-skill-scripting-language). Deterministic, sandboxed, Python-like |
| `hashicorp/go-plugin` | Plugin system over RPC | MPL-2.0 | Mature Go plugin framework for external process skills |
| `goccy/go-yaml` | YAML parsing for SKILL.yaml | MIT | Fast, spec-compliant YAML parser |
| `Masterminds/semver` | Semantic versioning | MIT | Skill version resolution and dependency compatibility |

### 7.4 External Tools (Optional Runtime)

| Tool | Purpose | Required? | Graceful Degradation |
|------|---------|-----------|---------------------|
| ripgrep (`rg`) | Fast regex code search | Optional | Go-native grep |
| tree-sitter CLI | Grammar installation | Bundled | Compiled into binary |
| mermaid-cli (`mmdc`) | Mermaid → SVG/PNG | Optional | Mermaid code blocks |
| `d2` | High-quality diagrams | Optional | Falls back to Mermaid |
| Hugo / MkDocs | Static site gen | Optional | Raw Markdown |
| semgrep | Additional SAST rules | Optional | Built-in tree-sitter |
| `xcodebuild` | Xcode project builds | macOS only | SPM-only via `swift build` |
| `xcrun simctl` | iOS Simulator management | macOS only | Disabled with message |
| `swift` | Swift compiler + SPM | Optional | Build/test features unavailable |
| `codesign` / `security` | Code signing introspection | macOS only | Signing tools disabled |
| `xcrun altool` / `notarytool` | App distribution & notarization | macOS only | Distribution features disabled |

### 7.5 LLM Provider APIs

All providers accessed via raw `net/http` + `bufio.Scanner`. No vendor SDKs. ~300 lines per provider.

| Provider | Endpoint | Streaming Format |
|----------|----------|-----------------|
| Anthropic | `POST /v1/messages` | SSE: `content_block_delta` events |
| OpenAI | `POST /v1/chat/completions` | SSE: `data: {"choices":[...]}` |
| Ollama | `POST /api/chat` | NDJSON |

---

## 8. Architecture Decision Records

### ADR-001: Go as the Implementation Language

**Status:** Accepted

**Context:** Choose a primary language for a CLI tool distributed as a single binary with concurrent I/O and deep terminal integration.

**Decision:** Go as the sole implementation language.

**Rationale:** Single binary distribution (zero runtime deps), goroutines/channels ideal for streaming + parallel execution, Charm ecosystem for TUI, trivial cross-compilation, millisecond startup time.

**Trade-offs:** Vs. Rust (better memory safety, steeper curve, slower compile, weaker TUI). Vs. TypeScript (richer LLM SDKs, requires Node, slower startup). Vs. Python (richest AI ecosystem, poor distribution, GIL).

**Consequences:** LLM API clients from scratch. CGO-free libraries preferred. Contributors must know Go.

---

### ADR-002: Shared Agent Core Across All Modes

**Status:** Accepted

**Context:** Three execution modes all need LLM interaction, tool execution, and conversation management.

**Decision:** All modes share a single Agent Core. Mode-specific behavior in thin I/O adapters.

**Rationale:** Bug fixes benefit all modes. Tools written once. Testing on one path.

**Consequences:** No UI dependencies in core. Features injected via interfaces. Skills work identically across all modes.

---

### ADR-003: Security Engine as a Standalone Subsystem

**Status:** Accepted

**Context:** Security analysis needed in code review, wiki, standalone audit, and CI gating.

**Decision:** Independent engine (`internal/security`). Consumers call it as a library. Security Rule Skills extend it.

**Rationale:** No detection logic duplication. Supports different scan scopes. Uniform `[]Finding` output. Skills add domain-specific rules.

**Consequences:** Finding struct serves all formats. Engine manages own LLM budget. Skill scanners via `SkillScannerAdapter`.

---

### ADR-004: Static + LLM Hybrid Approach for Security

**Status:** Accepted

**Context:** Static patterns (fast, free, limited) vs. LLM analysis (expensive, catches complex flaws).

**Decision:** Two-phase hybrid. Static first, LLM on prioritized segments. Security Rule Skills contribute to both phases.

**Rationale:** Static catches easy wins at zero cost. Informs LLM prioritization. LLMs catch auth flaws, business logic, data flows.

**Consequences:** Risk-scoring heuristic needed. LLM findings carry Confidence. Deduplication across static + LLM + skill findings.

---

### ADR-005: Mermaid as Default Diagram Format

**Status:** Accepted

**Context:** Wiki needs architecture, dependency, data flow, and sequence diagrams.

**Decision:** Mermaid default. D2 and DOT as options.

**Rationale:** Zero dependencies (renders natively in GitHub/GitLab). LLM-friendly. Human-editable.

**Consequences:** Diagrams as fenced code blocks. D2/DOT opt-in via CLI flag.

---

### ADR-006: No Vendor LLM SDKs

**Status:** Accepted

**Context:** No official Go SDKs for Anthropic/OpenAI. Community SDKs lag.

**Decision:** Implement from scratch using `net/http` + `bufio.Scanner`.

**Rationale:** Simple HTTP+SSE (~300 LOC per provider). Zero supply chain risk. Full control.

**Consequences:** Manual types. Manual new features. Thin `LLMProvider` interface insulates codebase.

---

### ADR-007: Tree-sitter for Multi-Language Code Analysis

**Status:** Accepted

**Context:** SAST scanner and wiki scanner need language-aware code analysis.

**Decision:** Tree-sitter via `smacker/go-tree-sitter`.

**Rationale:** 50+ languages one API. Incremental parsing. S-expression queries. Concrete syntax trees preserve positions.

**Consequences:** Grammars compiled into binary (+5-10MB). Skills can ship custom tree-sitter queries in SAST rules.

---

### ADR-008: Starlark as Skill Scripting Language

**Status:** Accepted

**Context:** Skills need a scripting language for rapid development. Options: Lua (gopher-lua), JavaScript (goja), Python (embedded CPython), Starlark, Wasm, Tengo.

**Decision:** Starlark as the primary embedded scripting language for skills, with external process as an escape hatch.

**Rationale:**

- **Deterministic execution:** No `while` loops, bounded recursion. Prevents infinite loops.
- **Sandboxed by design:** No filesystem, no network, no imports unless explicitly provided. Perfect for permission model.
- **Python-like syntax:** Skill authors who know Python can write Starlark immediately.
- **Battle-tested:** Used by Bazel (Google), Buck2 (Meta), Tilt. Proven at massive scale.
- **Excellent Go integration:** `go.starlark.net` is the official Google implementation. Stable, well-maintained.
- **Fast:** In-process, sub-millisecond for simple functions.

**Trade-offs:**

- **Vs. Lua:** Faster, but unfamiliar syntax. Starlark's Python-like syntax is more accessible.
- **Vs. JavaScript (goja):** More widely known, but incomplete ES6+ support and harder to sandbox.
- **Vs. Wasm:** Perfect isolation and any-language, but heavy runtime and limited debugging.

**Consequences:**

- Starlark skills use only host-provided functions. This enforces the permission model.
- Complex skills needing full libraries use the external process backend.
- We ship a Starlark helper library (`register_tool`, `register_hook`, `llm_complete`, etc.).

---

### ADR-009: Skill Permission Model

**Status:** Accepted

**Context:** Skills are third-party code extending the agent. Need to balance capability with security.

**Decision:** Declarative permission model with user approval on first use.

**Rationale:**

- **Declarative:** Skills declare required permissions in `SKILL.yaml`. Users evaluate risk before activation.
- **Minimal privilege:** Skills only get declared permissions. Prompt Skills with no I/O need zero permissions.
- **Progressive trust:** First use prompts; subsequent uses auto-approved (stored in SQLite). Revocable.
- **Headless compatible:** CI pre-approves via config or `--approve-skills` flag.
- **Auditable:** All grants logged with timestamps.

**Consequences:**

- Every Starlark SDK function checks permissions before execution.
- External process skills restricted based on declared permissions.
- Go plugin skills run in-process (higher trust, similar to installing any Go library).
- Registry should include "verified" badges for community-audited skills.

---

### ADR-010: Xcode CLI as Built-in Skill with Platform Gating

**Status:** Accepted

**Context:** iOS/macOS development is a major use case. Xcode CLI tools (`xcodebuild`, `xcrun simctl`, `codesign`, etc.) are essential for Apple platform developers but are only available on macOS. The agent must support Apple development without breaking on other platforms.

**Decision:** Implement Xcode CLI tools as a built-in multi-type skill (`apple-dev`) with runtime platform detection. The skill auto-activates when Apple project files are detected (`.xcodeproj`, `.xcworkspace`, `Package.swift`, `*.swift`). On non-macOS platforms, tools that require Xcode are disabled with clear messaging; cross-platform tools (`swift build`, `swift test`) remain available on Linux.

**Rationale:**

- **Built-in, not external:** Apple development is common enough to justify built-in support. Requiring users to install a separate skill creates unnecessary friction for a large user segment.
- **Multi-type skill:** Apple development needs tools (xcodebuild, simctl), domain knowledge (Swift best practices, SwiftUI patterns), and security rules (ATS, Keychain misuse). A single cohesive skill is better than three separate ones.
- **Platform gating over conditional compilation:** Using `runtime.GOOS` checks at runtime (not build tags) keeps a single binary for all platforms. The `apple-dev` skill simply deactivates its macOS-only tools on Linux/Windows.
- **xcodebuild over Xcode IDE:** CLI tools are automatable, CI-friendly, and don't require the full Xcode IDE GUI. They integrate naturally with the agent's shell execution model.
- **Build log parsing:** Raw `xcodebuild` output is verbose and hard for LLMs to process. Parsing it into structured errors/warnings/test results dramatically improves the agent's ability to diagnose and fix issues.
- **Simulator management:** `xcrun simctl` enables the agent to boot simulators, install/launch apps, capture screenshots, and stream logs — a complete iOS development workflow without leaving the terminal.

**Alternatives considered:**

- **External skill only:** Rejected. Too much friction for such a core use case. Apple developers would need to discover and install the skill manually.
- **Build-tag conditional compilation:** Rejected. Would produce different binaries per platform, complicating distribution. Runtime checks are simpler.
- **Direct Xcode IDE integration (AppleScript/XPC):** Rejected. Fragile, version-dependent, and conflicts with the terminal-first philosophy.
- **Fastlane integration:** Considered for future external skill. Fastlane adds Ruby dependency and is more suited for CI workflows than interactive development.

**Consequences:**

- Binary includes Apple tool wrappers on all platforms (~500 LOC overhead); they no-op on non-macOS.
- Swift/Objective-C tree-sitter grammars must be bundled for SAST and wiki code analysis.
- iOS security rules (ATS, Keychain, pasteboard, etc.) are always available regardless of platform (they scan source code, not runtime behavior).
- Future: a `fastlane` external skill could extend the built-in skill for CI/CD distribution workflows.

---

## 9. Implementation Roadmap

### Milestone 1: Core Agent (Weeks 1–4)

Goal: Interactive mode with basic tool support and one LLM provider.

| Task | Package | Effort |
|------|---------|--------|
| CLI entrypoint with Cobra | `cmd/agent` | 2 days |
| Bubble Tea TUI: input, streaming output, viewport | `internal/tui` | 5 days |
| Anthropic Messages API client with SSE | `internal/provider/anthropic` | 3 days |
| LLMProvider interface + StreamEvent types | `internal/provider` | 1 day |
| Agent loop (Plan → Act → Observe) | `internal/agent` | 3 days |
| Conversation manager + system prompt builder | `internal/agent` | 2 days |
| Tool interface + registry | `internal/tools` | 1 day |
| File read/write/patch tool | `internal/tools` | 2 days |
| Shell execution tool (with timeout) | `internal/tools` | 2 days |
| Permission gate (user approval) | `internal/tui` | 1 day |
| Config loading (TOML) + API key management | `internal/config` | 1 day |

### Milestone 2: Headless + Security (Weeks 5–8)

Goal: CI-ready code review, SRE log analysis, security scanning.

| Task | Package | Effort |
|------|---------|--------|
| Headless runner (stdin, --prompt, --file) | `internal/runner` | 2 days |
| Output formatters: JSON, Markdown, SARIF | `internal/output` | 3 days |
| Code review pipeline | `internal/pipeline` | 3 days |
| Log analysis pipeline | `internal/pipeline` | 2 days |
| GitHub PR comment formatter + API | `internal/output` | 2 days |
| Secret scanner (regex + entropy) | `internal/security/scanner` | 3 days |
| Dependency auditor (OSV API) | `internal/security/scanner` | 3 days |
| SAST pattern matcher (tree-sitter) | `internal/security/scanner` | 5 days |
| Config/infra scanner | `internal/security/scanner` | 2 days |
| Security engine orchestrator | `internal/security` | 2 days |
| Xcode CLI tools: xcodebuild, simctl, spm wrappers | `internal/tools/xcode` | 4 days |
| Build log parser (errors, warnings, test results) | `internal/tools/xcode` | 2 days |
| Apple platform security scanner (plist, ATS, entitlements) | `internal/security/scanner` | 3 days |
| iOS SAST rules (Keychain, pasteboard, WebView, etc.) | `internal/security/rules` | 2 days |
| OpenAI-compatible provider | `internal/provider/openai` | 2 days |

### Milestone 3: Skill System (Weeks 9–12)

Goal: Fully functional skill runtime with all five skill types.

| Task | Package | Effort |
|------|---------|--------|
| Skill manifest schema + YAML parser | `internal/skills` | 2 days |
| Skill loader + discovery (4 sources) | `internal/skills` | 3 days |
| Trigger evaluation engine | `internal/skills` | 2 days |
| Starlark engine + SDK built-in functions | `internal/skills` | 5 days |
| Starlark SDK: register_tool, register_hook, llm_complete | `internal/skills` | 3 days |
| Permission model + sandbox + approval store | `internal/skills` | 3 days |
| Lifecycle hook manager (9 hook phases) | `internal/skills` | 2 days |
| Tool Skill integration: dynamic tool registration | `internal/skills` | 2 days |
| Prompt Skill integration: system prompt injection | `internal/skills` | 1 day |
| Workflow Skill integration: multi-step orchestration | `internal/skills` | 3 days |
| Security Rule Skill: SkillScannerAdapter | `internal/security` | 2 days |
| Transform Skill: output post-processing | `internal/skills` | 1 day |
| Skill CLI: list, install, remove, create, info | `cmd/agent` | 2 days |
| External process skill backend (go-plugin) | `internal/skills` | 3 days |
| Built-in skills: core-tools, git, code-review, security-base | `internal/skills/builtin` | 3 days |
| 3 example skills (kubernetes, ddd-expert, rfc-writer) | `examples/skills/` | 2 days |

### Milestone 4: Wiki + Polish (Weeks 13–16)

Goal: Wiki generation with skill contributions, production hardening.

| Task | Package | Effort |
|------|---------|--------|
| Codebase scanner (tree-sitter + classification) | `internal/wiki` | 4 days |
| Context-aware chunker | `internal/wiki` | 2 days |
| Multi-pass LLM analyzer | `internal/wiki` | 5 days |
| Diagram generator (Mermaid) | `internal/wiki` | 3 days |
| Document assembler (+ skill wiki sections) | `internal/wiki` | 2 days |
| Site renderer (Hugo, MkDocs, raw MD) | `internal/wiki` | 3 days |
| LLM security analyzers (auth, dataflow, business) | `internal/security/analyzer` | 5 days |
| Attack chain correlator | `internal/security` | 2 days |
| Skill registry client | `internal/skills` | 2 days |
| MCP client + MCP-as-skill auto-discovery | `internal/tools/mcp` | 4 days |
| Ollama provider | `internal/provider/ollama` | 1 day |
| Context window manager | `internal/agent` | 3 days |
| SQLite persistence | `internal/store` | 2 days |
| Public Skill SDK package | `pkg/skillsdk` | 2 days |
| Go plugin skill backend | `internal/skills` | 2 days |
| Code signing introspection tools | `internal/tools/xcode` | 2 days |
| Xcode project wiki analyzer (targets, schemes, deps) | `internal/wiki` | 2 days |
| apple-dev built-in skill assembly + prompt files | `internal/skills/builtin` | 2 days |
| Test suite (>80% coverage) | `*/` | 5 days |
| Documentation + skill authoring guide | `docs/` | 3 days |

---

## 10. Risk Assessment

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| LLM API rate limits / downtime | High | Medium | Multi-provider fallback, retry, offline static scans |
| LLM hallucination in security findings | High | Medium | Confidence scoring, cross-reference static, label sources |
| Token cost explosion in wiki generation | Medium | Medium | Budget caps, prioritization, caching, incremental regen |
| Malicious skills executing harmful code | High | Low | Permission model, Starlark sandbox, process isolation, registry verification |
| Skill compat breaks across agent versions | Medium | Medium | SemVer in manifests, version checks at load time |
| Starlark limitations frustrating authors | Medium | Medium | External process escape hatch, clear docs |
| Tree-sitter grammar gaps | Low | Medium | Regex fallback, user-provided grammars |
| Context window overflow | Medium | High | Chunking, per-module analysis, summarization, depth config |
| False positives in security scanning | Medium | High | Entropy checks, confidence, .security.yaml overrides |
| Skill dependency conflicts | Low | Medium | SemVer resolution, clear errors, optional deps |
| Binary size growth | Low | High | Top 10 grammars bundled, lazy-download extras |
| Xcode version fragmentation | Medium | Medium | Test against last 2 major Xcode versions; version-sniff and adapt CLI flags |
| macOS-only tools confusing cross-platform users | Low | Medium | Clear "macOS required" messaging; SPM tools available on Linux; platform checks at skill activation |

---

## Appendix A: CLI Command Reference

### Interactive Mode

```bash
aiagent                                       # start interactive TUI
aiagent --model=claude-sonnet-4-5             # override model
aiagent --skills=kubernetes,docker            # activate specific skills
```

### Headless Mode

```bash
aiagent --headless --prompt "Explain the auth flow"
kubectl logs deploy/api | aiagent --headless --prompt "Find root cause"
aiagent --headless --mode=code-review --diff HEAD~1..HEAD --output=sarif
aiagent --headless --prompt "..." --max-turns=5 --tools=read,search --timeout=120s
aiagent --headless --prompt "..." --skills=kubernetes --approve-skills=kubernetes
```

### Wiki Generator

```bash
aiagent wiki --output=./docs/wiki
aiagent wiki --format=hugo --diagrams=mermaid --depth=deep
aiagent wiki --test-report --skills=hipaa-compliance
```

### Security Audit

```bash
aiagent security-audit
aiagent security-audit --output=sarif --scope=diff --diff HEAD~1..HEAD
aiagent security-audit --fail-on=high --skills=hipaa-compliance
```

### Apple Platform Development

```bash
# Interactive — auto-detects Xcode project and activates apple-dev skill
aiagent                                       # in a directory with *.xcodeproj

# Build and test via natural language
aiagent --prompt "Build the app and run all unit tests on iPhone 16 simulator"
aiagent --prompt "Why is my build failing? Fix the errors."

# Headless CI — Xcode build + test with structured output
aiagent --headless --mode=code-review --diff HEAD~1..HEAD --skills=apple-dev
aiagent --headless --prompt "Build and test MyApp scheme" --output=json --timeout=300s

# Simulator workflows
aiagent --prompt "Boot an iPhone 16 Pro simulator, build and install the app, then take a screenshot"

# Code signing diagnostics
aiagent --prompt "My archive is failing with a signing error. Diagnose and fix it."

# SPM on Linux (cross-platform subset)
aiagent --prompt "Add the Vapor dependency and run tests"  # works on Linux too
```

### Skill Management

```bash
aiagent skill list                            # list installed skills
aiagent skill list --available                # list registry skills
aiagent skill search "kubernetes"             # search registry
aiagent skill install kubernetes              # install from registry
aiagent skill install kubernetes@1.2.0        # specific version
aiagent skill install github.com/co/my-skill  # from git
aiagent skill install ./local-skill           # from local path
aiagent skill add kubernetes                  # add to project (.agent/skills/)
aiagent skill remove kubernetes               # uninstall
aiagent skill info kubernetes                 # show manifest + permissions
aiagent skill create my-skill --type=tool     # scaffold new skill
aiagent skill test ./my-skill                 # run skill tests
aiagent skill permissions kubernetes          # show/manage permissions
aiagent skill permissions kubernetes --revoke # revoke all permissions
```

---

## Appendix B: Configuration Reference

```toml
# ~/.config/aiagent/config.toml

[provider]
default = "anthropic"
model = "claude-sonnet-4-5"

[provider.anthropic]
api_key_source = "keyring"

[provider.openai]
base_url = "https://api.openai.com/v1"
api_key_source = "env"

[provider.ollama]
base_url = "http://localhost:11434"

[agent]
max_turns = 50
approval_mode = "prompt"
context_budget = 100000

[security]
fail_on = "high"
custom_rules = ".security.yaml"
enable_llm_analysis = true
max_llm_calls = 20

[wiki]
format = "raw-md"
diagrams = "mermaid"
depth = "standard"

[skills]
auto_approve = ["core-tools", "git"]
user_skills_dir = "~/.config/aiagent/skills"
registry_url = "https://github.com/aiagent-skills/registry"
max_llm_calls_per_skill = 5
max_shell_exec_per_skill = 10
skill_shell_timeout = "30s"

[skills.config.kubernetes]
default_namespace = "production"
kubeconfig_path = "~/.kube/config"

[skills.config.i18n-output]
language = "ja"
```

### Project Security Rules (`.security.yaml`)

```yaml
rules:
  - id: "CUSTOM-001"
    title: "Direct DB queries forbidden — use repository layer"
    severity: high
    pattern:
      type: "regex"
      match: 'db\.(Query|Exec|QueryRow)\('
      exclude_paths: ["internal/repository/"]

dependencies:
  banned:
    - package: "github.com/dgrijalva/jwt-go"
      reason: "Unmaintained. Use github.com/golang-jwt/jwt/v5"

overrides:
  - id: "GO-CRYPTO-001"
    severity: "info"
    note: "MD5 used only for cache keys, not security"

ci:
  fail_on: "high"
  max_findings: 10
```

---

## Appendix C: Unified Finding Schema

```go
type Finding struct {
    ID          string
    Scanner     string            // includes skill-provided scanner name
    Severity    Severity          // critical | high | medium | low | info
    Category    Category
    Title       string
    Description string
    Location    Location          // { File, StartLine, EndLine, Function }
    CWE         string
    OWASP       string
    Evidence    string
    Remediation string
    Confidence  Confidence        // high | medium | low
    References  []string
    Metadata    map[string]string
    SkillSource string            // skill that produced this (empty for built-in)
}

type AttackChain struct {
    ID         string
    Title      string
    Severity   Severity
    Steps      []Finding
    Impact     string
    Likelihood string
}
```

**Categories:** `injection`, `authentication`, `authorization`, `cryptography`, `secrets-exposure`, `vulnerable-dependency`, `misconfiguration`, `data-exposure`, `race-condition`, `input-validation`, `logging-monitoring`, `supply-chain`, `license-compliance`

---

## Appendix D: Wiki Output Structure

```
docs/wiki/
├── _index.md
├── architecture/
│   ├── overview.md, data-flow.md, dependencies.md
├── modules/
│   ├── _index.md, ...per-module pages
├── code-structure/
│   ├── overview.md, key-abstractions.md, design-patterns.md
├── testing/
│   ├── coverage-report.md, test-execution.md, testing-gaps.md
├── security/
│   ├── overview.md, findings.md, attack-chains.md
│   ├── dependencies.md, auth-review.md, data-flow.md
│   ├── recommendations.md, sbom.md
├── suggestions/
│   ├── improvements.md, security.md, performance.md
├── skill-contributed/          # Sections contributed by active skills
│   ├── kubernetes-architecture.md
│   ├── hipaa-compliance.md
│   └── xcode-project-structure.md  # from apple-dev skill
└── glossary.md
```

---

## Appendix E: Skill Manifest Reference

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique skill identifier (lowercase, hyphens) |
| `version` | string | SemVer version |
| `description` | string | One-line description |
| `types` | []string | `tool`, `prompt`, `workflow`, `security-rule`, `transform` |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `author` | string | Author name or organization |
| `license` | string | SPDX license identifier |
| `homepage` | string | URL to skill repo |
| `triggers` | object | Auto-activation: files, keywords, modes, languages |
| `permissions` | []string | Required permissions |
| `dependencies` | []object | Other skills required |
| `implementation` | object | Backend: starlark, plugin, process |
| `prompt` | object | system_prompt_file, context_files, max_context_tokens |
| `tools` | []object | name, description, input_schema_file, requires_approval |
| `security_rules` | object | sast_rules_file, scanners, overrides_file |
| `wiki` | object | sections, diagrams |
| `compatibility` | object | agent_version, platforms |

---

## Appendix F: Skill SDK API Reference

### Starlark Built-in Functions

| Function | Permission | Description |
|----------|-----------|-------------|
| `read_file(path) → string` | `file:read` | Read file contents |
| `write_file(path, content)` | `file:write` | Write file |
| `list_dir(path) → []string` | `file:read` | List directory |
| `search_files(pattern) → []string` | `file:read` | Glob search |
| `exec(cmd, *args) → ExecResult` | `shell:exec` | Run shell command |
| `fetch(url, **opts) → Response` | `net:fetch` | HTTP request |
| `llm_complete(prompt) → string` | `llm:call` | LLM completion |
| `llm_json(prompt, schema) → dict` | `llm:call` | LLM with JSON output |
| `git_diff(base, head) → string` | `git:read` | Git diff |
| `git_log(n) → []Commit` | `git:read` | Recent commits |
| `git_status() → []FileStatus` | `git:read` | Working tree status |
| `env(key) → string` | `env:read` | Read env variable |
| `invoke_skill(name, func, args) → any` | `skill:invoke` | Call another skill |
| `project_root() → string` | *none* | Project root path |
| `project_language() → string` | *none* | Detected primary language |
| `load_file(path) → string` | *none* | Load file relative to skill dir |
| `log(level, msg)` | *none* | Log message |
| `register_tool(name, handler, **opts)` | *none* | Register tool for LLM |
| `register_hook(phase, handler)` | *none* | Register lifecycle hook |
| `register_workflow(name, handler)` | *none* | Register workflow |
| `register_scanner(name, handler)` | *none* | Register security scanner |
| `hook_result(**opts) → HookResult` | *none* | Create hook result |
| `error(msg) → Error` | *none* | Return error from handler |

---

## Appendix G: Xcode Tool JSON Schemas

### xcode_build / xcode_test / xcode_archive / xcode_clean

```json
{
  "type": "object",
  "properties": {
    "workspace": {
      "type": "string",
      "description": "Path to .xcworkspace file (mutually exclusive with project)"
    },
    "project": {
      "type": "string",
      "description": "Path to .xcodeproj file (mutually exclusive with workspace)"
    },
    "scheme": {
      "type": "string",
      "description": "Build scheme name"
    },
    "destination": {
      "type": "string",
      "description": "Build destination (e.g., 'platform=iOS Simulator,name=iPhone 16'). Defaults to latest iOS simulator.",
      "default": "platform=iOS Simulator,name=iPhone 16"
    },
    "configuration": {
      "type": "string",
      "enum": ["Debug", "Release"],
      "description": "Build configuration",
      "default": "Debug"
    },
    "extra_args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Additional xcodebuild arguments"
    }
  },
  "required": ["scheme"]
}
```

### sim_list

```json
{
  "type": "object",
  "properties": {
    "runtime": {
      "type": "string",
      "description": "Filter by runtime (e.g., 'iOS 18.0', 'watchOS 11.0')"
    },
    "state": {
      "type": "string",
      "enum": ["Booted", "Shutdown"],
      "description": "Filter by device state"
    }
  }
}
```

### sim_boot / sim_shutdown

```json
{
  "type": "object",
  "properties": {
    "device_name": {
      "type": "string",
      "description": "Simulator device name (e.g., 'iPhone 16 Pro')"
    },
    "device_udid": {
      "type": "string",
      "description": "Simulator device UDID (alternative to device_name)"
    }
  }
}
```

### sim_install

```json
{
  "type": "object",
  "properties": {
    "device_name": { "type": "string" },
    "device_udid": { "type": "string" },
    "app_path": {
      "type": "string",
      "description": "Path to .app bundle to install"
    }
  },
  "required": ["app_path"]
}
```

### sim_launch

```json
{
  "type": "object",
  "properties": {
    "device_name": { "type": "string" },
    "device_udid": { "type": "string" },
    "bundle_id": {
      "type": "string",
      "description": "App bundle identifier (e.g., 'com.example.MyApp')"
    }
  },
  "required": ["bundle_id"]
}
```

### sim_screenshot

```json
{
  "type": "object",
  "properties": {
    "device_name": { "type": "string" },
    "device_udid": { "type": "string" },
    "output_path": {
      "type": "string",
      "description": "Path to save the screenshot PNG",
      "default": "/tmp/simulator-screenshot.png"
    }
  }
}
```

### sim_log

```json
{
  "type": "object",
  "properties": {
    "device_name": { "type": "string" },
    "device_udid": { "type": "string" },
    "predicate": {
      "type": "string",
      "description": "NSPredicate filter for log messages (e.g., 'subsystem == \"com.example.MyApp\"')"
    },
    "last_minutes": {
      "type": "integer",
      "description": "Show logs from the last N minutes",
      "default": 5
    }
  }
}
```

### swift_build / swift_test

```json
{
  "type": "object",
  "properties": {
    "package_path": {
      "type": "string",
      "description": "Path to directory containing Package.swift",
      "default": "."
    },
    "configuration": {
      "type": "string",
      "enum": ["debug", "release"],
      "default": "debug"
    },
    "target": {
      "type": "string",
      "description": "Specific target to build/test (omit for all)"
    },
    "extra_args": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}
```

### swift_resolve

```json
{
  "type": "object",
  "properties": {
    "package_path": {
      "type": "string",
      "default": "."
    }
  }
}
```

### swift_add_dep

```json
{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "Git URL of the Swift package (e.g., 'https://github.com/vapor/vapor.git')"
    },
    "version": {
      "type": "string",
      "description": "Version requirement (e.g., '4.0.0', 'from: \"4.0.0\"', '.upToNextMajor(from: \"4.0.0\")')"
    },
    "package_path": {
      "type": "string",
      "default": "."
    }
  },
  "required": ["url"]
}
```

### codesign_info

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "enum": ["identities", "profiles", "entitlements"],
      "description": "What signing information to retrieve"
    },
    "app_path": {
      "type": "string",
      "description": "Path to .app bundle (for entitlements query)"
    },
    "team_id": {
      "type": "string",
      "description": "Filter by team ID"
    }
  },
  "required": ["query"]
}
```

### codesign_verify

```json
{
  "type": "object",
  "properties": {
    "app_path": {
      "type": "string",
      "description": "Path to .app bundle to verify"
    },
    "deep": {
      "type": "boolean",
      "description": "Verify all nested code (frameworks, plugins)",
      "default": true
    }
  },
  "required": ["app_path"]
}
```
