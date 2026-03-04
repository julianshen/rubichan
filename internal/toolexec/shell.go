package toolexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandPart represents a single simple command extracted from a shell string.
type CommandPart struct {
	Prefix string // command name (first word after env vars)
	Full   string // full command with arguments
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
	engine *RuleEngine
}

// NewShellValidator creates a ShellValidator backed by the given RuleEngine.
func NewShellValidator(engine *RuleEngine) *ShellValidator {
	return &ShellValidator{engine: engine}
}

// Validate parses the command string and checks each sub-command against the
// rule engine with CategoryBash. Returns an error if any sub-command is denied.
func (v *ShellValidator) Validate(_ context.Context, command string) error {
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

			if err := validator.Validate(ctx, command); err != nil {
				return Result{
					Content: fmt.Sprintf("shell command blocked: %s", err),
					IsError: true,
				}
			}

			return next(ctx, tc)
		}
	}
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
