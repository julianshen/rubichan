package skills

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/commands"
)

type declarativeSlashCommand struct {
	skillName string
	def       CommandDef
}

func newDeclarativeSlashCommand(skillName string, def CommandDef) commands.SlashCommand {
	return &declarativeSlashCommand{
		skillName: skillName,
		def:       def,
	}
}

func (c *declarativeSlashCommand) Name() string {
	return c.def.Name
}

func (c *declarativeSlashCommand) Description() string {
	return c.def.Description
}

func (c *declarativeSlashCommand) Arguments() []commands.ArgumentDef {
	args := make([]commands.ArgumentDef, 0, len(c.def.Arguments))
	for _, arg := range c.def.Arguments {
		args = append(args, commands.ArgumentDef{
			Name:        arg.Name,
			Description: arg.Description,
			Required:    arg.Required,
		})
	}
	return args
}

func (c *declarativeSlashCommand) Complete(_ context.Context, _ []string) []commands.Candidate {
	return nil
}

func (c *declarativeSlashCommand) Execute(_ context.Context, args []string) (commands.Result, error) {
	missing := make([]string, 0, len(c.def.Arguments))
	for i, arg := range c.def.Arguments {
		if arg.Required && i >= len(args) {
			missing = append(missing, arg.Name)
		}
	}
	if len(missing) > 0 {
		return commands.Result{}, fmt.Errorf("missing required arguments: %s", strings.Join(missing, ", "))
	}

	result := fmt.Sprintf("Skill command %q from %q invoked", c.def.Name, c.skillName)
	if len(args) > 0 {
		result += " with args: " + strings.Join(args, ", ")
	}
	if c.def.Description != "" {
		result += "\n" + c.def.Description
	}

	return commands.Result{Output: result}, nil
}

func manifestCommands(manifest *SkillManifest) []commands.SlashCommand {
	if manifest == nil {
		return nil
	}
	cmds := make([]commands.SlashCommand, 0, len(manifest.Commands))
	for _, def := range manifest.Commands {
		cmds = append(cmds, newDeclarativeSlashCommand(manifest.Name, def))
	}
	return cmds
}

func manifestAgentDefinitions(manifest *SkillManifest) []*AgentDefinition {
	if manifest == nil {
		return nil
	}
	defs := make([]*AgentDefinition, 0, len(manifest.Agents))
	for _, agent := range manifest.Agents {
		tools := make([]string, len(agent.Tools))
		copy(tools, agent.Tools)
		defs = append(defs, &AgentDefinition{
			Name:          agent.Name,
			Description:   agent.Description,
			SystemPrompt:  agent.SystemPrompt,
			Tools:         tools,
			MaxTurns:      agent.MaxTurns,
			MaxDepth:      agent.MaxDepth,
			Model:         agent.Model,
			InheritSkills: agent.InheritSkills,
			ExtraSkills:   append([]string(nil), agent.ExtraSkills...),
			DisableSkills: append([]string(nil), agent.DisableSkills...),
		})
	}
	return defs
}
