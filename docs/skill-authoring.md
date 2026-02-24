# Skill Authoring Guide

This guide covers everything you need to create, test, and publish skills for Rubichan.

## 1. Quick Start

The fastest way to create a skill is with the `skill create` command:

```bash
rubichan skill create my-skill
```

This scaffolds a directory with a `SKILL.yaml` manifest and a `skill.star` entrypoint. You can also create a minimal **prompt skill** by hand -- no backend code required:

```
my-skill/
  SKILL.yaml
  prompts/
    system.md
```

```yaml
# SKILL.yaml
name: ddd-expert
version: 0.1.0
description: "Domain-Driven Design expert guidance"
types:
  - prompt
prompt:
  system_prompt_file: prompts/system.md
```

Write your system prompt in `prompts/system.md`, point `rubichan` at the skill directory, and the prompt is injected into every conversation:

```bash
rubichan --skills=./my-skill/
```

## 2. Skill Types

Every skill declares one or more types in its `SKILL.yaml`. There are five types:

### tool

Provides new tools the agent can invoke during conversations. Requires a backend implementation.

```yaml
# SKILL.yaml
name: kubernetes
version: 0.1.0
description: "Kubernetes management tools"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
permissions:
  - shell:exec
```

```python
# skill.star
def kubectl_get(input):
    result = exec("kubectl", "get", input["resource"])
    return result["stdout"]

register_tool(
    name = "kubectl_get",
    description = "Get Kubernetes resources by type (pods, services, deployments, etc.)",
    handler = kubectl_get,
)
```

### prompt

Injects system prompt context into conversations. No backend needed -- just a YAML manifest pointing at markdown files.

```yaml
name: ddd-expert
version: 0.1.0
description: "Domain-Driven Design expert guidance"
types:
  - prompt
prompt:
  system_prompt_file: prompts/system.md
  context_files:
    - patterns/aggregate.md
    - patterns/repository.md
  max_context_tokens: 4000
```

### workflow

Multi-step automated workflows that combine tools, LLM calls, and file operations.

```yaml
name: rfc-writer
version: 0.1.0
description: "RFC document writer workflow"
types:
  - workflow
implementation:
  backend: starlark
  entrypoint: skill.star
permissions:
  - file:write
  - llm:call
```

```python
# skill.star
def write_rfc(input):
    title = input["title"]
    context = input["context"]
    prompt = "Write an RFC document titled '{}'. Context: {}".format(title, context)
    prompt += "\n\nInclude these sections: Summary, Motivation, Detailed Design, "
    prompt += "Alternatives Considered, and Open Questions."
    draft = llm_complete(prompt)
    filename = "rfc-{}.md".format(title.lower().replace(" ", "-"))
    write_file(filename, draft)
    return filename

register_workflow(
    name = "write_rfc",
    handler = write_rfc,
)
```

### security-rule

Custom security scanning rules that plug into the security engine.

```yaml
name: secret-scanner
version: 0.1.0
description: "Custom secret detection rules"
types:
  - security-rule
implementation:
  backend: starlark
  entrypoint: scanner.star
permissions:
  - file:read
```

```python
# scanner.star
def scan_for_tokens(content):
    findings = []
    for line in content.split("\n"):
        if "PRIVATE_KEY" in line or "SECRET" in line:
            findings.append("Potential secret found: " + line.strip()[:80])
    return findings

register_scanner(
    name = "token_scanner",
    handler = scan_for_tokens,
)
```

### transform

Code or text transformation pipelines. Implemented the same way as tools but semantically indicates the skill transforms input content.

## 3. Starlark Guide

Starlark is Rubichan's primary skill scripting language. It is a Python-like language designed for deterministic, sandboxed execution.

### Available Builtins

**Registration functions** -- called at the top level of your `.star` file:

| Function | Signature | Purpose |
|---|---|---|
| `register_tool` | `register_tool(name, description, handler)` | Register a tool the agent can call |
| `register_workflow` | `register_workflow(name, handler)` | Register a multi-step workflow |
| `register_hook` | `register_hook(phase, handler)` | Register a lifecycle hook |
| `register_scanner` | `register_scanner(name, handler)` | Register a security scanner |

**SDK functions** -- available inside handlers:

| Function | Signature | Permission | Description |
|---|---|---|---|
| `read_file` | `read_file(path)` | `file:read` | Read file contents (max 10 MB) |
| `write_file` | `write_file(path, content)` | `file:write` | Write content to a file |
| `list_dir` | `list_dir(path)` | `file:read` | List directory entries |
| `search_files` | `search_files(pattern)` | `file:read` | Glob pattern file search |
| `exec` | `exec(command, *args)` | `shell:exec` | Run a command (30s timeout) |
| `llm_complete` | `llm_complete(prompt)` | `llm:call` | Send prompt to the LLM |
| `fetch` | `fetch(url)` | `net:fetch` | HTTP GET a URL |
| `git_diff` | `git_diff(*args)` | `git:read` | Run git diff |
| `git_log` | `git_log(*args)` | `git:read` | Get commit log entries |
| `git_status` | `git_status()` | `git:read` | Get working tree status |
| `env` | `env(key)` | `env:read` | Read an environment variable |
| `project_root` | `project_root()` | (none) | Get the project root path |
| `invoke_skill` | `invoke_skill(name, input_dict)` | `skill:invoke` | Call another skill |
| `log` | `log(message)` | (none) | Write to the agent log |

### Return Values

- `exec()` returns a dict: `{"stdout": "...", "stderr": "...", "exit_code": 0}`
- `git_log()` returns a list of dicts: `[{"hash": "...", "author": "...", "message": "..."}]`
- `git_status()` returns a list of dicts: `[{"path": "...", "status": "M"}]`

### Sandbox Constraints

- File paths are resolved relative to the skill directory and cannot escape it.
- Commands run directly without a shell wrapper, preventing shell injection. Commands time out after 30 seconds.
- No network access without the `net:fetch` permission.
- No filesystem access outside the skill directory without explicit permissions.
- Starlark is deterministic: no `import`, no threads, no random, no system clock.

### Hook Phases

Hooks let skills intercept agent lifecycle events. Register them with `register_hook`:

```python
def on_before_tool(event):
    tool_name = event["data"].get("tool_name", "")
    log("About to call tool: " + tool_name)
    return {"modified": {}, "cancel": False}

register_hook(phase = "OnBeforeToolCall", handler = on_before_tool)
```

Available phases: `OnActivate`, `OnDeactivate`, `OnConversationStart`, `OnBeforePromptBuild`, `OnBeforeToolCall`, `OnAfterToolResult`, `OnAfterResponse`, `OnBeforeWikiSection`, `OnSecurityScanComplete`.

Hook handlers receive an event dict with `phase`, `skill_name`, and `data` keys. They must return a dict with `modified` (data to feed back) and `cancel` (boolean to abort the operation).

## 4. Permissions

Skills must declare every permission they need in `SKILL.yaml`. The user is prompted to approve permissions the first time a skill runs.

### Permission Reference

| Permission | Scope | Description |
|---|---|---|
| `file:read` | Project files | Read files within the project directory |
| `file:write` | Project files | Create or modify files within the project |
| `shell:exec` | System | Run shell commands |
| `net:fetch` | Network | Make outbound HTTP requests |
| `llm:call` | LLM provider | Invoke the LLM for completions |
| `git:read` | Git repo | Read git status, log, and diffs |
| `git:write` | Git repo | Create commits and branches |
| `env:read` | Environment | Read environment variables |
| `env:write` | Environment | Set environment variables |
| `skill:invoke` | Skills | Call other installed skills |

### Approval Flow

1. When a skill is activated, Rubichan displays the list of requested permissions.
2. The user approves or denies each permission (or all at once).
3. Approvals are persisted in the local SQLite store (`~/.config/rubichan/skills.db`).
4. If a skill calls a builtin that requires a permission it was not granted, the call fails with a permission error.

Manage approvals with the CLI:

```bash
rubichan skill permissions kubernetes        # list approvals
rubichan skill permissions kubernetes --revoke  # revoke all
```

### Principle of Least Privilege

Only declare permissions your skill actually uses. A prompt skill needs zero permissions. A tool that reads files needs only `file:read`. Users are more likely to trust and install skills with minimal permission footprints.

## 5. Testing

### Validate the Manifest

```bash
rubichan skill test ./my-skill/
```

This parses `SKILL.yaml` and reports validation errors (missing fields, invalid names, unknown permissions or backends).

### Run with Local Skills

Load your skill directory directly without installing:

```bash
rubichan --skills=./my-skill/
```

This activates the skill for the session. You can specify multiple skills:

```bash
rubichan --skills=./skill-a/,./skill-b/
```

### Headless Testing

For CI or automated validation, use headless mode:

```bash
rubichan --headless --prompt "Use kubectl_get to list pods" \
    --skills=./kubernetes/ --approve-skills=kubernetes
```

The `--approve-skills` flag auto-approves permissions for the named skills, avoiding interactive prompts in CI.

### Scaffold and Iterate

The scaffolded `skill.star` from `rubichan skill create` includes a working `hello` tool you can test immediately:

```bash
rubichan skill create my-tool
rubichan --skills=./my-tool/
# Then ask the agent: "Use the hello tool with name 'Rubichan'"
```

## 6. Publishing

### Install from Local Path

During development, install your skill from a local directory:

```bash
rubichan skill install ./my-skill/
```

This copies the skill to `~/.config/rubichan/skills/<name>/` and records it in the store.

### Install from Registry

Install published skills by name:

```bash
rubichan skill install kubernetes
rubichan skill install kubernetes@1.2.0      # pin exact version
rubichan skill install kubernetes@^1.0.0     # SemVer range
rubichan skill install kubernetes@~1.2       # compatible patch versions
```

SemVer ranges (`^`, `~`, `>=`, etc.) are resolved against the registry's available versions.

### Install from Git

Skills can also be installed directly from git repositories:

```bash
rubichan skill install https://github.com/user/my-skill.git
```

The repository must contain a valid `SKILL.yaml` at the root.

### Search and Discover

```bash
rubichan skill search kubernetes
rubichan skill list --available              # list all registry skills
rubichan skill list                          # list installed skills
```

### Versioning

Follow [Semantic Versioning](https://semver.org/):

- **MAJOR** (1.0.0 -> 2.0.0): breaking changes to tool schemas or behavior
- **MINOR** (1.0.0 -> 1.1.0): new tools, workflows, or features (backward compatible)
- **PATCH** (1.0.0 -> 1.0.1): bug fixes, documentation updates

The `version` field in `SKILL.yaml` must be a valid SemVer string. The `compatibility.agent_version` field can specify which Rubichan versions your skill supports.

### Manage Skills

```bash
rubichan skill info kubernetes               # show manifest details
rubichan skill remove kubernetes             # uninstall
rubichan skill add ./my-skill/               # add to current project (.agent/skills/)
```

## 7. Reference

### SKILL.yaml Full Schema

```yaml
# Required fields
name: my-skill                    # lowercase, letters/digits/hyphens, max 128 chars
version: 1.0.0                    # semantic version
description: "What this skill does"
types:                            # at least one required
  - tool                          # tool | prompt | workflow | security-rule | transform

# Optional metadata
author: "Your Name"
license: MIT
homepage: "https://github.com/you/my-skill"

# Auto-activation triggers
triggers:
  files: ["Dockerfile", "*.k8s.yaml"]      # activate when these files exist
  keywords: ["kubernetes", "kubectl"]       # activate on these conversation keywords
  modes: ["interactive", "headless"]        # restrict to specific modes
  languages: ["go", "python"]              # activate for these project languages

# Permissions (declare all that are needed)
permissions:
  - file:read
  - shell:exec

# Skill dependencies
dependencies:
  - name: other-skill
    version: "^1.0.0"
    optional: false

# Implementation (required for non-prompt types)
implementation:
  backend: starlark              # starlark | plugin | process | mcp
  entrypoint: skill.star         # path relative to skill directory

# Prompt configuration (for prompt skills)
prompt:
  system_prompt_file: prompts/system.md
  context_files: ["context/patterns.md"]
  max_context_tokens: 4000

# Tool declarations (optional, for documentation)
tools:
  - name: kubectl_get
    description: "Get Kubernetes resources"
    input_schema_file: schemas/kubectl_get.json
    requires_approval: false

# Security rule configuration
security_rules:
  sast_rules_file: rules/sast.yaml
  scanners:
    - name: token_scanner
      entrypoint: scanner.star
  overrides_file: overrides.yaml

# Wiki contributions
wiki:
  sections:
    - title: "Kubernetes Architecture"
      template: templates/k8s-arch.md
      analyzer: analyzers/k8s.star
  diagrams:
    - type: deployment
      template: templates/deploy-diagram.md

# Compatibility constraints
compatibility:
  agent_version: ">=0.5.0"
  platforms: ["darwin", "linux"]
```

### Backend Types

| Backend | Use Case | Details |
|---|---|---|
| `starlark` | Most skills | Sandboxed Python-like scripting, deterministic |
| `plugin` | Performance-critical | Go shared library (`.so`), implements `pkg/skillsdk.SkillPlugin` |
| `process` | Any language | External process communicating via JSON-RPC |
| `mcp` | MCP servers | Model Context Protocol server integration |

### Go Plugin SDK

For Go plugin backends, import `pkg/skillsdk` and implement the `SkillPlugin` interface:

```go
package main

import "github.com/julianshen/rubichan/pkg/skillsdk"

type MySkill struct{}

func (s *MySkill) Manifest() skillsdk.Manifest {
    return skillsdk.Manifest{
        Name:        "my-plugin",
        Version:     "1.0.0",
        Description: "A Go plugin skill",
    }
}

func (s *MySkill) Activate(ctx skillsdk.Context) error {
    // ctx provides: ReadFile, WriteFile, ListDir, SearchFiles,
    //   Exec, Complete, Fetch, GitDiff, GitLog, GitStatus,
    //   GetEnv, ProjectRoot, InvokeSkill
    return nil
}

func (s *MySkill) Deactivate(ctx skillsdk.Context) error {
    return nil
}

// NewSkill is the entry point the runtime calls to load the plugin.
func NewSkill() skillsdk.SkillPlugin {
    return &MySkill{}
}
```

Build as a shared library:

```bash
go build -buildmode=plugin -o my-plugin.so ./
```

### Further Reading

- **spec.md** -- Section 4 (Skill System) for the full specification
- **pkg/skillsdk/** -- Go plugin SDK source and godoc
- **internal/skills/starlark/** -- Starlark engine implementation and builtins
- **internal/skills/manifest.go** -- Manifest parsing and validation logic
