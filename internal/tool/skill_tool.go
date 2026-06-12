package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"late/internal/common"
	"late/internal/skill"
	"os"
	"path/filepath"
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

func (t ActivateSkillTool) Name() string        { return "activate_skill" }
func (t ActivateSkillTool) Description() string { return "Activate a skill by name to see its instructions and available scripts." }
func (t ActivateSkillTool) Parameters() json.RawMessage {
	names := make([]string, 0, len(t.Skills))
	var descBuilder strings.Builder
	descBuilder.WriteString("The name of the skill to activate. Available skills:\n")

	for name, s := range t.Skills {
		names = append(names, name)
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

	// Resolve ${{SKILL_DIR}} placeholders so the agent knows the absolute
	// file-system location of the skill directory.
	instructions := strings.ReplaceAll(s.Instructions, "${{SKILL_DIR}}", s.Path)

	// List available scripts but do NOT register them as tools.
	// The agent invokes scripts via the bash tool, which respects
	// workstation constraints (conda env, custom interpreters, etc.).
	scriptsDir := filepath.Join(s.Path, "scripts")
	var scriptsSection string
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		var scriptPaths []string
		for _, entry := range entries {
			if !entry.IsDir() {
				scriptPath := filepath.Join(scriptsDir, entry.Name())
				scriptPaths = append(scriptPaths, scriptPath)
			}
		}
		if len(scriptPaths) > 0 {
			var sb strings.Builder
			sb.WriteString("\nAvailable scripts (invoke via the bash tool):\n")
			for _, sp := range scriptPaths {
				sb.WriteString(fmt.Sprintf("  - %s\n", sp))
			}
			sb.WriteString("\nChoose the correct interpreter for your environment")
			sb.WriteString(" (python/node/etc.) and construct the bash command accordingly.")
			scriptsSection = sb.String()
		}
	}

	return fmt.Sprintf(
		"Skill '%s' activated.\n\n"+
			"Skill Directory: %s\n"+
			"Instructions:\n%s%s",
		s.Metadata.Name,
		s.Path,
		instructions,
		scriptsSection,
	), nil
}

func (t ActivateSkillTool) RequiresConfirmation(args json.RawMessage) bool { return false }
func (t ActivateSkillTool) CallString(args json.RawMessage) string {
	var params struct {
		Name string `json:"name"`
	}
	json.Unmarshal(args, &params)
	return fmt.Sprintf("Activating skill '%s'", params.Name)
}
