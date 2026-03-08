package toolexec

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandPart represents a single simple command extracted from a shell string.
type CommandPart struct {
	Prefix string // command name (first word after env vars)
	Full   string // full command with arguments
}

// ShellInterception captures pre-execution safety findings for a shell command.
type ShellInterception struct {
	BlockReason string
	RouteReason string
	Warnings    []string
}

// ParseCommand parses a shell command string into its component simple commands.
// It uses mvdan.cc/sh/v3/syntax to build a full AST, then walks the tree
// extracting every *syntax.CallExpr as a CommandPart.
func ParseCommand(command string) ([]CommandPart, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil
	}

	parser := syntax.NewParser(syntax.KeepComments(false))
	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, fmt.Errorf("parse shell command: %w", err)
	}

	var parts []CommandPart
	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}

		// Skip CallExprs that have only assignments and no args (pure env set).
		if len(call.Args) == 0 {
			return true
		}

		words := wordStrings(call.Args)
		if len(words) == 0 {
			return true
		}

		prefix := words[0]
		full := strings.Join(words, " ")

		parts = append(parts, CommandPart{
			Prefix: prefix,
			Full:   full,
		})

		// Detect bash -c / sh -c and recursively parse the argument.
		if (prefix == "bash" || prefix == "sh") && len(words) >= 3 && words[1] == "-c" {
			inner := words[2]
			innerParts, innerErr := ParseCommand(inner)
			if innerErr == nil {
				parts = append(parts, innerParts...)
			}
		}

		return true
	})

	if len(parts) == 0 {
		return nil, nil
	}
	return parts, nil
}

// wordStrings converts a slice of syntax.Word nodes into their string
// representations. It handles Lit values and SglQuoted values, stripping
// quotes to produce the logical command text.
func wordStrings(words []*syntax.Word) []string {
	result := make([]string, 0, len(words))
	for _, w := range words {
		s := wordToString(w)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// wordToString converts a single syntax.Word node to its string value.
func wordToString(w *syntax.Word) string {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				switch ip := inner.(type) {
				case *syntax.Lit:
					b.WriteString(ip.Value)
				}
			}
		}
	}
	return b.String()
}

// ShellValidator validates shell commands by parsing them into sub-commands
// and checking each against the rule engine.
type ShellValidator struct {
	engine  *RuleEngine
	workDir string
}

// NewShellValidator creates a ShellValidator backed by the given RuleEngine.
func NewShellValidator(engine *RuleEngine, workDir string) *ShellValidator {
	return &ShellValidator{
		engine:  engine,
		workDir: workDir,
	}
}

// Validate parses the command string and checks each sub-command against the
// rule engine with CategoryBash. Returns an error if any sub-command is denied.
func (v *ShellValidator) Validate(ctx context.Context, command string) error {
	interception, err := v.Inspect(ctx, command)
	if err != nil {
		return err
	}
	if interception.RouteReason != "" {
		return fmt.Errorf("command requires routing: %s", interception.RouteReason)
	}
	if interception.BlockReason != "" {
		return fmt.Errorf("command blocked: %s", interception.BlockReason)
	}

	parts, err := ParseCommand(command)
	if err != nil {
		return fmt.Errorf("shell validation parse error: %w", err)
	}

	for _, part := range parts {
		input := json.RawMessage(fmt.Sprintf(`{"command":%q}`, part.Full))
		action := v.engine.Evaluate(CategoryBash, "shell", input)
		if action == ActionDeny {
			return fmt.Errorf("sub-command denied: %s", part.Full)
		}
	}
	return nil
}

var (
	redirectPattern = regexp.MustCompile(`(?i)\b(?:echo|cat)\b[^;\n]*\s(?:>>?)\s*[^\s;]+`)
	sedInPlaceRegex = regexp.MustCompile(`(?i)\bsed\b[^;\n]*\s-i(?:\s|$)`)
	chmodChownRegex = regexp.MustCompile(`(?i)\b(?:chmod|chown)\b`)
	mvCpOutside     = regexp.MustCompile(`(?i)\b(?:mv|cp)\b[^;\n]*(?:\s/\S+|\s\.\./\S+)`)
)

// Inspect evaluates shell-specific safety checks that sit alongside rule
// engine deny rules. It returns route/block/warn decisions for the command.
func (v *ShellValidator) Inspect(_ context.Context, command string) (ShellInterception, error) {
	var interception ShellInterception

	parts, err := ParseCommand(command)
	if err != nil {
		return interception, fmt.Errorf("shell validation parse error: %w", err)
	}

	for _, part := range parts {
		if part.Prefix == "apply_patch" {
			interception.RouteReason = "apply_patch shell commands must be routed through the file tool"
			break
		}
	}

	if interception.BlockReason == "" {
		if outsideTargets := findRecursiveRMOutsideWorkdir(parts, v.workDir); len(outsideTargets) > 0 {
			interception.BlockReason = fmt.Sprintf("recursive rm target(s) escape working directory: %s", strings.Join(outsideTargets, ", "))
		}
	}

	if redirectPattern.MatchString(command) {
		interception.Warnings = append(interception.Warnings, "command redirects output to a file")
	}
	if sedInPlaceRegex.MatchString(command) {
		interception.Warnings = append(interception.Warnings, "command uses sed -i for in-place file edits")
	}
	if chmodChownRegex.MatchString(command) {
		interception.Warnings = append(interception.Warnings, "command changes file ownership/permissions")
	}
	if mvCpOutside.MatchString(command) {
		interception.Warnings = append(interception.Warnings, "command may move/copy files outside the working directory")
	}

	return interception, nil
}

// ShellSafetyMiddleware returns a Middleware that validates shell commands
// before execution. It only activates for CategoryBash tool calls.
func ShellSafetyMiddleware(validator *ShellValidator) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if CategoryFromContext(ctx) != CategoryBash {
				return next(ctx, tc)
			}

			command := extractCommandField(tc.Input)
			if command == "" {
				return next(ctx, tc)
			}

			interception, err := validator.Inspect(ctx, command)
			if err != nil {
				return Result{
					Content: fmt.Sprintf("shell command blocked: %s", err),
					IsError: true,
				}
			}

			if interception.RouteReason != "" {
				return Result{
					Content: fmt.Sprintf("shell command requires routing: %s. Use the file tool for this operation.", interception.RouteReason),
					IsError: true,
				}
			}
			if interception.BlockReason != "" {
				return Result{
					Content: fmt.Sprintf("shell command blocked: %s. Use the file tool for file edits.", interception.BlockReason),
					IsError: true,
				}
			}
			if err := validator.validateRuleEngine(command); err != nil {
				return Result{
					Content: fmt.Sprintf("shell command blocked: %s", err),
					IsError: true,
				}
			}

			result := next(ctx, tc)
			if len(interception.Warnings) == 0 {
				return result
			}

			prefix := formatInterceptionWarnings(interception.Warnings)
			result.Content = prefix + result.Content
			if result.DisplayContent != "" {
				result.DisplayContent = prefix + result.DisplayContent
			}
			return result
		}
	}
}

func (v *ShellValidator) validateRuleEngine(command string) error {
	if v.engine == nil {
		return nil
	}

	parts, err := ParseCommand(command)
	if err != nil {
		return fmt.Errorf("shell validation parse error: %w", err)
	}

	for _, part := range parts {
		input := json.RawMessage(fmt.Sprintf(`{"command":%q}`, part.Full))
		action := v.engine.Evaluate(CategoryBash, "shell", input)
		if action == ActionDeny {
			return fmt.Errorf("sub-command denied: %s", part.Full)
		}
	}
	return nil
}

func findRecursiveRMOutsideWorkdir(parts []CommandPart, workDir string) []string {
	var outside []string
	for _, part := range parts {
		if part.Prefix != "rm" {
			continue
		}

		fields := strings.Fields(part.Full)
		if len(fields) <= 1 {
			continue
		}

		recursive := false
		targets := make([]string, 0, len(fields)-1)
		parseTargets := false
		for _, token := range fields[1:] {
			if token == "" {
				continue
			}
			if parseTargets {
				targets = append(targets, token)
				continue
			}
			if token == "--" {
				parseTargets = true
				continue
			}
			if strings.HasPrefix(token, "--") {
				if token == "--recursive" {
					recursive = true
				}
				continue
			}
			if strings.HasPrefix(token, "-") {
				if strings.Contains(token, "r") {
					recursive = true
				}
				continue
			}
			targets = append(targets, token)
		}

		if !recursive {
			continue
		}
		for _, target := range targets {
			if isOutsideWorkdir(target, workDir) {
				outside = append(outside, target)
			}
		}
	}
	return outside
}

func isOutsideWorkdir(target, workDir string) bool {
	if target == "" || target == "-" {
		return false
	}
	if strings.HasPrefix(target, "~") {
		return true
	}

	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return false
	}

	var absTarget string
	if filepath.IsAbs(target) {
		absTarget = filepath.Clean(target)
	} else {
		absTarget = filepath.Clean(filepath.Join(workDir, target))
	}
	absWorkDir := filepath.Clean(workDir)

	return !strings.HasPrefix(absTarget, absWorkDir+string(filepath.Separator)) && absTarget != absWorkDir
}

func formatInterceptionWarnings(warnings []string) string {
	var b strings.Builder
	b.WriteString("warning: shell safety interceptor detected file-modifying pattern(s):\n")
	for _, warning := range warnings {
		b.WriteString("- ")
		b.WriteString(warning)
		b.WriteByte('\n')
	}
	return b.String()
}

// extractCommandField extracts the "command" string value from JSON input.
func extractCommandField(input json.RawMessage) string {
	var obj struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &obj); err != nil {
		return ""
	}
	return obj.Command
}
