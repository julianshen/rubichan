package skills

import (
	"path/filepath"
	"sort"
	"strings"
)

// TriggerContext holds the runtime context used to evaluate which skills
// should be auto-activated. It captures information about the current project,
// user input, and execution mode.
type TriggerContext struct {
	// ProjectFiles is the list of filenames (basenames) present in the project.
	ProjectFiles []string

	// CurrentPath is the currently focused file path, if known.
	CurrentPath string

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

// ActivationScore breaks down how strongly a skill matches the current trigger context.
type ActivationScore struct {
	Explicit    int
	CurrentPath int
	Files       int
	Keywords    int
	Languages   int
	Modes       int
	Total       int
}

// ActivationReport describes whether and why a skill activated for a trigger context.
type ActivationReport struct {
	Skill     DiscoveredSkill
	Activated bool
	Score     ActivationScore

	MatchedFiles     []string
	MatchedKeywords  []string
	MatchedLanguages []string
	MatchedModes     []string
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
	reports := EvaluateTriggerReports(skills, ctx, 1)
	matched := make([]DiscoveredSkill, 0, len(reports))
	for _, report := range reports {
		if report.Activated {
			matched = append(matched, report.Skill)
		}
	}
	return matched
}

// EvaluateTriggerReports scores each skill and returns an ordered report. Skills
// at or above threshold are marked Activated. Reports are sorted by score
// descending and then by name for deterministic behavior.
func EvaluateTriggerReports(skills []DiscoveredSkill, ctx TriggerContext, threshold int) []ActivationReport {
	if threshold <= 0 {
		threshold = 1
	}

	reports := make([]ActivationReport, 0, len(skills))
	for _, skill := range skills {
		report := scoreSkillActivation(skill, ctx)
		report.Activated = report.Score.Total >= threshold
		reports = append(reports, report)
	}

	sort.SliceStable(reports, func(i, j int) bool {
		if reports[i].Score.Total == reports[j].Score.Total {
			return reports[i].Skill.Manifest.Name < reports[j].Skill.Manifest.Name
		}
		return reports[i].Score.Total > reports[j].Score.Total
	})

	return reports
}

func scoreSkillActivation(skill DiscoveredSkill, ctx TriggerContext) ActivationReport {
	report := ActivationReport{Skill: skill}

	// Explicit (inline) skills always activate and should dominate sort order.
	if skill.Source == SourceInline {
		report.Score.Explicit = 1000
		report.Score.Total = 1000
		return report
	}

	triggers := skill.Manifest.Triggers

	// A skill with no triggers never auto-activates.
	if len(triggers.Files) == 0 &&
		len(triggers.Keywords) == 0 &&
		len(triggers.Languages) == 0 &&
		len(triggers.Modes) == 0 {
		return report
	}

	report.MatchedFiles = matchedFiles(triggers.Files, ctx.ProjectFiles)
	if len(report.MatchedFiles) > 0 {
		report.Score.Files = len(report.MatchedFiles) * 100
	}

	report.Score.CurrentPath = currentPathScore(triggers.Files, ctx.CurrentPath)

	report.MatchedKeywords = matchedKeywords(triggers.Keywords, ctx.LastUserMessage)
	if len(report.MatchedKeywords) > 0 {
		report.Score.Keywords = len(report.MatchedKeywords) * 80
	}

	report.MatchedLanguages = matchedExact(triggers.Languages, ctx.DetectedLangs)
	if len(report.MatchedLanguages) > 0 {
		report.Score.Languages = len(report.MatchedLanguages) * 60
	}

	report.MatchedModes = matchedModes(triggers.Modes, ctx.Mode)
	if len(report.MatchedModes) > 0 {
		report.Score.Modes = len(report.MatchedModes) * 40
	}

	report.Score.Total = report.Score.Explicit +
		report.Score.CurrentPath +
		report.Score.Files +
		report.Score.Keywords +
		report.Score.Languages +
		report.Score.Modes
	return report
}

// matchesFiles returns true if any trigger pattern matches any project file
// using filepath.Match (shell glob semantics).
func matchedFiles(patterns, projectFiles []string) []string {
	seen := make(map[string]bool)
	var matched []string
	for _, pattern := range patterns {
		for _, file := range projectFiles {
			// filepath.Match matches against the basename only.
			ok, err := filepath.Match(pattern, filepath.Base(file))
			if err == nil && ok && !seen[pattern] {
				seen[pattern] = true
				matched = append(matched, pattern)
			}
		}
	}
	return matched
}

func currentPathScore(patterns []string, currentPath string) int {
	if currentPath == "" {
		return 0
	}
	base := filepath.Base(currentPath)
	for _, pattern := range patterns {
		if pattern == base || pattern == currentPath {
			return 150
		}
		if ok, err := filepath.Match(pattern, base); err == nil && ok {
			return 100
		}
		if ok, err := filepath.Match(pattern, currentPath); err == nil && ok {
			return 100
		}
	}
	return 0
}

// matchesKeywords returns true if any keyword is found in the message
// using case-insensitive substring matching.
func matchedKeywords(keywords []string, message string) []string {
	lowerMsg := strings.ToLower(message)
	var matched []string
	for _, kw := range keywords {
		if strings.Contains(lowerMsg, strings.ToLower(kw)) {
			matched = append(matched, kw)
		}
	}
	return matched
}

// matchesExact returns true if any trigger value matches any detected value
// using exact string comparison.
func matchedExact(triggerValues, detectedValues []string) []string {
	seen := make(map[string]bool)
	var matched []string
	for _, tv := range triggerValues {
		for _, dv := range detectedValues {
			if tv == dv && !seen[tv] {
				seen[tv] = true
				matched = append(matched, tv)
			}
		}
	}
	return matched
}

// matchesMode returns true if any trigger mode matches the current mode exactly.
func matchedModes(modes []string, currentMode string) []string {
	var matched []string
	for _, m := range modes {
		if m == currentMode {
			matched = append(matched, m)
		}
	}
	return matched
}
