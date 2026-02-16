package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSkill is a test helper that creates a DiscoveredSkill with the given
// name, source, and trigger configuration.
func makeSkill(name string, source Source, triggers TriggerConfig) DiscoveredSkill {
	return DiscoveredSkill{
		Manifest: &SkillManifest{
			Name:        name,
			Version:     "1.0.0",
			Description: "Skill " + name,
			Types:       []SkillType{SkillTypePrompt},
			Triggers:    triggers,
		},
		Dir:    "/fake/" + name,
		Source: source,
	}
}

func TestTriggerExplicit(t *testing.T) {
	// Explicit (SourceInline) skills always match regardless of triggers.
	skill := makeSkill("explicit-skill", SourceInline, TriggerConfig{})

	ctx := TriggerContext{
		ProjectFiles:    []string{"main.go"},
		DetectedLangs:   []string{"go"},
		BuildSystem:     "go",
		LastUserMessage: "hello",
		Mode:            "interactive",
		ExplicitSkills:  []string{"explicit-skill"},
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	require.Len(t, result, 1)
	assert.Equal(t, "explicit-skill", result[0].Manifest.Name)
}

func TestTriggerFileMatch(t *testing.T) {
	// A skill with file triggers should match when project files match the pattern.
	skill := makeSkill("docker-skill", SourceProject, TriggerConfig{
		Files: []string{"Dockerfile", "docker-compose*.yml"},
	})

	ctx := TriggerContext{
		ProjectFiles:    []string{"README.md", "Dockerfile", "main.go"},
		DetectedLangs:   []string{"go"},
		BuildSystem:     "go",
		LastUserMessage: "build my project",
		Mode:            "interactive",
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	require.Len(t, result, 1)
	assert.Equal(t, "docker-skill", result[0].Manifest.Name)
}

func TestTriggerFileNoMatch(t *testing.T) {
	// A skill with file triggers should NOT match when no project files match.
	skill := makeSkill("docker-skill", SourceProject, TriggerConfig{
		Files: []string{"Dockerfile", "docker-compose*.yml"},
	})

	ctx := TriggerContext{
		ProjectFiles:    []string{"README.md", "main.go", "go.mod"},
		DetectedLangs:   []string{"go"},
		BuildSystem:     "go",
		LastUserMessage: "build my project",
		Mode:            "interactive",
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	assert.Empty(t, result)
}

func TestTriggerKeywordMatch(t *testing.T) {
	// A skill with keyword triggers should match when the user message contains
	// one of the keywords (case-insensitive).
	skill := makeSkill("k8s-skill", SourceUser, TriggerConfig{
		Keywords: []string{"kubernetes", "k8s"},
	})

	ctx := TriggerContext{
		ProjectFiles:    []string{"main.go"},
		DetectedLangs:   []string{"go"},
		BuildSystem:     "go",
		LastUserMessage: "Deploy my app to Kubernetes please",
		Mode:            "interactive",
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	require.Len(t, result, 1)
	assert.Equal(t, "k8s-skill", result[0].Manifest.Name)
}

func TestTriggerLanguageMatch(t *testing.T) {
	// A skill with language triggers should match when a detected language matches exactly.
	skill := makeSkill("rust-skill", SourceProject, TriggerConfig{
		Languages: []string{"rust"},
	})

	ctx := TriggerContext{
		ProjectFiles:    []string{"main.rs", "Cargo.toml"},
		DetectedLangs:   []string{"rust"},
		BuildSystem:     "cargo",
		LastUserMessage: "help me fix this code",
		Mode:            "interactive",
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	require.Len(t, result, 1)
	assert.Equal(t, "rust-skill", result[0].Manifest.Name)
}

func TestTriggerModeMatch(t *testing.T) {
	// A skill with mode triggers should match when the current mode matches exactly.
	skill := makeSkill("ci-skill", SourceUser, TriggerConfig{
		Modes: []string{"headless"},
	})

	ctx := TriggerContext{
		ProjectFiles:    []string{"main.go"},
		DetectedLangs:   []string{"go"},
		BuildSystem:     "go",
		LastUserMessage: "run tests",
		Mode:            "headless",
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	require.Len(t, result, 1)
	assert.Equal(t, "ci-skill", result[0].Manifest.Name)
}

func TestTriggerNoTriggers(t *testing.T) {
	// A skill with empty triggers should never auto-activate (unless explicit).
	skill := makeSkill("passive-skill", SourceProject, TriggerConfig{})

	ctx := TriggerContext{
		ProjectFiles:    []string{"main.go", "Dockerfile"},
		DetectedLangs:   []string{"go", "dockerfile"},
		BuildSystem:     "go",
		LastUserMessage: "do everything",
		Mode:            "interactive",
	}

	result := EvaluateTriggers([]DiscoveredSkill{skill}, ctx)
	assert.Empty(t, result)
}

func TestEvaluateMultipleSkills(t *testing.T) {
	// Given multiple skills, only those whose triggers match should be returned.
	goSkill := makeSkill("go-skill", SourceProject, TriggerConfig{
		Languages: []string{"go"},
	})
	rustSkill := makeSkill("rust-skill", SourceProject, TriggerConfig{
		Languages: []string{"rust"},
	})
	dockerSkill := makeSkill("docker-skill", SourceProject, TriggerConfig{
		Files: []string{"Dockerfile"},
	})
	passiveSkill := makeSkill("passive-skill", SourceProject, TriggerConfig{})
	explicitSkill := makeSkill("explicit-skill", SourceInline, TriggerConfig{})

	all := []DiscoveredSkill{goSkill, rustSkill, dockerSkill, passiveSkill, explicitSkill}

	ctx := TriggerContext{
		ProjectFiles:    []string{"main.go", "Dockerfile"},
		DetectedLangs:   []string{"go"},
		BuildSystem:     "go",
		LastUserMessage: "build it",
		Mode:            "interactive",
		ExplicitSkills:  []string{"explicit-skill"},
	}

	result := EvaluateTriggers(all, ctx)

	// Should match: go-skill (language), docker-skill (file), explicit-skill (inline source).
	// Should NOT match: rust-skill (wrong lang), passive-skill (no triggers).
	require.Len(t, result, 3)

	names := make(map[string]bool)
	for _, s := range result {
		names[s.Manifest.Name] = true
	}
	assert.True(t, names["go-skill"], "go-skill should match via language trigger")
	assert.True(t, names["docker-skill"], "docker-skill should match via file trigger")
	assert.True(t, names["explicit-skill"], "explicit-skill should match via inline source")
	assert.False(t, names["rust-skill"], "rust-skill should not match")
	assert.False(t, names["passive-skill"], "passive-skill should not match")
}
