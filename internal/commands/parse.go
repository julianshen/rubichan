package commands

import "fmt"

// ParseLine splits a slash command line into shell-like arguments, preserving
// quoted strings so multi-word prompts can be passed to commands.
func ParseLine(line string) ([]string, error) {
	var (
		args    []string
		current []rune
		quote   rune
		escape  bool
	)

	flush := func() {
		if len(current) == 0 {
			return
		}
		args = append(args, string(current))
		current = current[:0]
	}

	for _, r := range line {
		switch {
		case escape:
			current = append(current, r)
			escape = false
		case r == '\\':
			escape = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current = append(current, r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			current = append(current, r)
		}
	}

	if escape {
		return nil, fmt.Errorf("unterminated escape sequence")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}

	flush()
	return args, nil
}
