package commands

import (
	"context"
	"fmt"
	"strings"
)

// SkillInfo holds minimal skill information for the /skill command.
type SkillInfo struct {
	Name        string
	Description string
	Source      string
	State       string
}

// SkillLister provides skill information without importing the skills package.
// This interface breaks the import cycle (skills already imports commands).
type SkillLister interface {
	ListSkills() []SkillInfo
	ActivateSkill(name string) error
	DeactivateSkill(name string) error
}

type skillCommand struct {
	lister SkillLister
}

// NewSkillCommand creates a command to list, activate, and deactivate skills.
func NewSkillCommand(lister SkillLister) SlashCommand {
	return &skillCommand{lister: lister}
}

func (c *skillCommand) Name() string        { return "skill" }
func (c *skillCommand) Description() string { return "Manage skills" }

func (c *skillCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{
		{
			Name:        "subcommand",
			Description: "list | activate <name> | deactivate <name>",
			Required:    false,
		},
	}
}

func (c *skillCommand) Complete(_ context.Context, args []string) []Candidate {
	if len(args) == 0 {
		return []Candidate{
			{Value: "list", Description: "List all skills"},
			{Value: "activate", Description: "Activate a skill"},
			{Value: "deactivate", Description: "Deactivate a skill"},
		}
	}

	// Second argument: skill names for activate/deactivate.
	sub := args[0]
	if sub == "activate" || sub == "deactivate" {
		skills := c.lister.ListSkills()
		candidates := make([]Candidate, 0, len(skills))
		for _, sk := range skills {
			candidates = append(candidates, Candidate{
				Value:       sk.Name,
				Description: sk.Description,
			})
		}
		return candidates
	}

	return nil
}

func (c *skillCommand) Execute(_ context.Context, args []string) (Result, error) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "list":
		return c.executeList()
	case "activate":
		if len(args) < 2 {
			return Result{}, fmt.Errorf("skill name is required: /skill activate <name>")
		}
		return c.executeActivate(args[1])
	case "deactivate":
		if len(args) < 2 {
			return Result{}, fmt.Errorf("skill name is required: /skill deactivate <name>")
		}
		return c.executeDeactivate(args[1])
	default:
		return Result{}, fmt.Errorf("unknown subcommand %q: use list, activate, or deactivate", sub)
	}
}

func (c *skillCommand) executeList() (Result, error) {
	skills := c.lister.ListSkills()
	if len(skills) == 0 {
		return Result{Output: "No skills discovered."}, nil
	}

	var b strings.Builder
	b.WriteString("Skills:\n")
	for _, sk := range skills {
		fmt.Fprintf(&b, "  %-30s %-10s %-10s %s\n", sk.Name, sk.State, sk.Source, sk.Description)
	}
	return Result{Output: b.String()}, nil
}

func (c *skillCommand) executeActivate(name string) (Result, error) {
	if err := c.lister.ActivateSkill(name); err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("Skill %q activated.", name)}, nil
}

func (c *skillCommand) executeDeactivate(name string) (Result, error) {
	if err := c.lister.DeactivateSkill(name); err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("Skill %q deactivated.", name)}, nil
}
