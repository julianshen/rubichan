package verification

import "strings"

// DependencyResolutionCommand returns the normalized command string when it
// matches a known dependency-resolution pattern, or an empty string otherwise.
func DependencyResolutionCommand(command string) string {
	command = strings.TrimSpace(strings.ToLower(command))
	if command == "" {
		return ""
	}

	switch {
	case strings.Contains(command, "mvn "):
		if strings.Contains(command, "dependency:resolve") ||
			strings.Contains(command, "compile") ||
			strings.Contains(command, "package") ||
			strings.Contains(command, "test") {
			return command
		}
		return ""
	case strings.Contains(command, "npm ci"),
		strings.Contains(command, "npm install"),
		strings.Contains(command, "pnpm install"),
		strings.Contains(command, "yarn install"),
		strings.Contains(command, "go get"),
		strings.Contains(command, "go mod tidy"),
		strings.Contains(command, "gradle build"),
		strings.Contains(command, "gradlew build"),
		strings.Contains(command, "./gradlew build"),
		strings.Contains(command, "pip install"),
		strings.Contains(command, "uv sync"),
		strings.Contains(command, "poetry install"):
		return command
	default:
		return ""
	}
}

// LooksLikeDependencyCommandIntent reports whether the command appears to be
// trying to install/resolve dependencies, even if the exact known pattern does
// not match.
func LooksLikeDependencyCommandIntent(command string) bool {
	command = strings.ToLower(command)
	return strings.Contains(command, "install") ||
		strings.Contains(command, "npm i") ||
		strings.Contains(command, "pnpm i") ||
		strings.Contains(command, "yarn add") ||
		strings.Contains(command, "pip") ||
		strings.Contains(command, "go mod") ||
		strings.Contains(command, "go get") ||
		strings.Contains(command, "mvn") ||
		strings.Contains(command, "gradle") ||
		strings.Contains(command, "gradlew")
}
