package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"late/internal/common"
	"late/internal/skill"
	"sort"
	"strings"
)

// ActivateSkillTool is a tool that "activates" a skill — the agent receives
// the skill's instructions and a list of available scripts to invoke via the
// bash tool.  Scripts are NOT registered as separate tools so the agent can
// choose the correct interpreter (python/node/conda env) based on its
// workstation constraints.
type ActivateSkillTool struct {
	Skills map[string]*skill.Skill
	Reg    *common.ToolRegistry // kept for backwards compat, no longer used
}

func (t ActivateSkillTool) Name() string { return "activate_skill" }
func (t ActivateSkillTool) Description() string {
	return "Activate a skill by name to see its instructions and enable its scripts as tools."
}
func (t ActivateSkillTool) Parameters() json.RawMessage {
	names := make([]string, 0, len(t.Skills))
	var descBuilder strings.Builder
	descBuilder.WriteString("The name of the skill to activate. Available skills:\n")

	for name := range t.Skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		s := t.Skills[name]
		descBuilder.WriteString(fmt.Sprintf("- %s: %s\n", name, s.Metadata.Description))
	}
	enumStr, _ := json.Marshal(names)

	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"name": { 
				"type": "string", 
				"enum": %s, 
				"description": %q 
			}
		},
		"required": ["name"]
	}`, string(enumStr), descBuilder.String()))
}

func (t ActivateSkillTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	s, ok := t.Skills[params.Name]
	if !ok {
		return fmt.Sprintf("Skill '%s' not found", params.Name), nil
	}

	// Resolve placeholders so the agent sees absolute paths — exactly
	// as WorkBuddy does. Both ${{SKILL_DIR}} and bare SKILL_DIR are
	// supported for compatibility.
	instructions := s.Instructions
	instructions = strings.ReplaceAll(instructions, "${{SKILL_DIR}}", s.Path)
	instructions = strings.ReplaceAll(instructions, "SKILL_DIR", s.Path)

	refs := skill.DiscoverSkillReferences(s)
	var resp strings.Builder
	resp.WriteString(fmt.Sprintf("Skill '%s' activated.\n\nInstructions:\n%s", s.Metadata.Name, s.Instructions))
	if len(refs) > 0 {
		resp.WriteString("\n\n## Available References\n")
		for _, ref := range refs {
			resp.WriteString(fmt.Sprintf("- `%s`\n", ref))
		}
		resp.WriteString("\nTo read a reference file, use the `skill_read_reference` tool with the skill name and file path.\n")
	}
	return resp.String(), nil
}

func (t ActivateSkillTool) RequiresConfirmation(args json.RawMessage) bool { return false }
func (t ActivateSkillTool) CallString(args json.RawMessage) string {
	var params struct {
		Name string `json:"name"`
	}
	json.Unmarshal(args, &params)
	return fmt.Sprintf("Activating skill '%s'", params.Name)
}
