package skills

import (
	"path/filepath"
	"strings"
)

// TriggerContext holds the runtime context used to evaluate which skills
// should be auto-activated. It captures information about the current project,
// user input, and execution mode.
type TriggerContext struct {
	// ProjectFiles is the list of filenames (basenames) present in the project.
	ProjectFiles []string

	// DetectedLangs is the list of programming languages detected in the project.
	DetectedLangs []string

	// BuildSystem is the detected build system (e.g. "go", "cargo", "npm").
	BuildSystem string

	// LastUserMessage is the most recent message from the user.
	LastUserMessage string

	// Mode is the current execution mode (e.g. "interactive", "headless", "wiki").
	Mode string

	// ExplicitSkills lists skill names explicitly requested by the user (e.g. via --skills flag).
	ExplicitSkills []string
}

// EvaluateTriggers filters a list of discovered skills to those that should
// be activated given the current trigger context.
//
// A skill is activated if any of the following are true:
//   - The skill's Source is SourceInline (explicitly requested).
//   - Any file trigger pattern matches a project file (using filepath.Match).
//   - Any keyword trigger appears in the user message (case-insensitive).
//   - Any language trigger matches a detected language (exact match).
//   - Any mode trigger matches the current mode (exact match).
//
// A skill with no triggers defined is never auto-activated (unless it is inline/explicit).
func EvaluateTriggers(skills []DiscoveredSkill, ctx TriggerContext) []DiscoveredSkill {
	var matched []DiscoveredSkill

	for _, skill := range skills {
		if shouldActivate(skill, ctx) {
			matched = append(matched, skill)
		}
	}

	return matched
}

// shouldActivate determines whether a single skill should be activated
// given the current trigger context.
func shouldActivate(skill DiscoveredSkill, ctx TriggerContext) bool {
	// Explicit (inline) skills always activate.
	if skill.Source == SourceInline {
		return true
	}

	triggers := skill.Manifest.Triggers

	// A skill with no triggers never auto-activates.
	if len(triggers.Files) == 0 &&
		len(triggers.Keywords) == 0 &&
		len(triggers.Languages) == 0 &&
		len(triggers.Modes) == 0 {
		return false
	}

	// Any matching trigger activates the skill.
	if matchesFiles(triggers.Files, ctx.ProjectFiles) {
		return true
	}
	if matchesKeywords(triggers.Keywords, ctx.LastUserMessage) {
		return true
	}
	if matchesExact(triggers.Languages, ctx.DetectedLangs) {
		return true
	}
	if matchesMode(triggers.Modes, ctx.Mode) {
		return true
	}

	return false
}

// matchesFiles returns true if any trigger pattern matches any project file
// using filepath.Match (shell glob semantics).
func matchesFiles(patterns, projectFiles []string) bool {
	for _, pattern := range patterns {
		for _, file := range projectFiles {
			// filepath.Match matches against the basename only.
			matched, err := filepath.Match(pattern, filepath.Base(file))
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

// matchesKeywords returns true if any keyword is found in the message
// using case-insensitive substring matching.
func matchesKeywords(keywords []string, message string) bool {
	lowerMsg := strings.ToLower(message)
	for _, kw := range keywords {
		if strings.Contains(lowerMsg, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// matchesExact returns true if any trigger value matches any detected value
// using exact string comparison.
func matchesExact(triggerValues, detectedValues []string) bool {
	for _, tv := range triggerValues {
		for _, dv := range detectedValues {
			if tv == dv {
				return true
			}
		}
	}
	return false
}

// matchesMode returns true if any trigger mode matches the current mode exactly.
func matchesMode(modes []string, currentMode string) bool {
	for _, m := range modes {
		if m == currentMode {
			return true
		}
	}
	return false
}
