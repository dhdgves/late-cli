package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"late/internal/common"
	"late/internal/skill"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ScriptTool executes a script from a skill's scripts/ directory.
type ScriptTool struct {
	SkillName  string
	ScriptName string
	ScriptPath string
}

func (t ScriptTool) Name() string {
	// sanitized script name to be used as tool name
	return fmt.Sprintf("skill_%s_%s", t.SkillName, sanitizeToolName(t.ScriptName))
}

func (t ScriptTool) Description() string {
	return fmt.Sprintf("Execute the '%s' script from the '%s' skill. Script path: %s", t.ScriptName, t.SkillName, t.ScriptPath)
}

func (t ScriptTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"args": { "type": "array", "items": { "type": "string" }, "description": "Arguments to pass to the script" }
		}
	}`)
}

func (t ScriptTool) Execute(ctx context.Context, args json.RawMessage) (result string, execErr error) {
	// Catch panics from platform-specific script execution or encoding
	// so a single bad script never takes down the agent.
	defer func() {
		if r := recover(); r != nil {
			result = ""
			execErr = fmt.Errorf("script tool panicked: %v", r)
		}
	}()

	var params struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Determine how to run the script based on extension.
	ext := filepath.Ext(t.ScriptPath)
	interpreter := resolveInterpreter(ext)
	if interpreter == "" {
		// No known interpreter — assume the script is directly executable
		// (e.g. .sh on Unix, .ps1 on Windows via assoc).
		interpreter = t.ScriptPath
		params.Args = nil
	}

	var cmd *exec.Cmd
	if interpreter == t.ScriptPath {
		cmd = exec.CommandContext(ctx, interpreter, params.Args...)
	} else {
		cmd = exec.CommandContext(ctx, interpreter, append([]string{t.ScriptPath}, params.Args...)...)
	}

	output, err := cmd.CombinedOutput()

	// Convert legacy encodings before returning to the TUI.
	output = DetectAndConvert(output)

	if err != nil {
		return fmt.Sprintf("Script failed with error: %v\nOutput: %s", err, string(output)), nil
	}

	return string(output), nil
}

// resolveInterpreter returns the path to the interpreter for the given
// script extension, resolving platform-specific binary names.
// Returns "" if the extension is unknown.
func resolveInterpreter(ext string) string {
	switch ext {
	case ".py":
		return resolvePython()
	case ".js":
		return resolveNode()
	default:
		return ""
	}
}

// resolvePython finds a usable Python 3 interpreter.
// On Unix: python3 → python → python3 (bare string, let exec fail)
// On Windows: python3.exe → python.exe → python (let exec fail)
func resolvePython() string {
	if runtime.GOOS == "windows" {
		for _, name := range []string{"python3.exe", "python.exe"} {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
		return "python.exe"
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "python3"
}

// resolveNode finds a usable Node.js interpreter.
func resolveNode() string {
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("node.exe"); err == nil {
			return p
		}
	}
	return "node"
}

func (t ScriptTool) RequiresConfirmation(args json.RawMessage) bool {
	return true // Always require confirmation for skill scripts for safety
}

func (t ScriptTool) CallString(args json.RawMessage) string {
	return fmt.Sprintf("Running script '%s' from skill '%s'", t.ScriptName, t.SkillName)
}

// ActivateSkillTool is a tool that "activates" a skill.
type ActivateSkillTool struct {
	Skills map[string]*skill.Skill
	Reg    *common.ToolRegistry
}

func (t ActivateSkillTool) Name() string        { return "activate_skill" }
func (t ActivateSkillTool) Description() string { return "Activate a skill by name to see its instructions and enable its scripts as tools." }
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

	// Register scripts as tools
	scriptsDir := filepath.Join(s.Path, "scripts")
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				scriptPath := filepath.Join(scriptsDir, entry.Name())
				st := ScriptTool{
					SkillName:  s.Metadata.Name,
					ScriptName: entry.Name(),
					ScriptPath: scriptPath,
				}
				t.Reg.Register(st)
			}
		}
	}

	// Resolve ${{SKILL_DIR}} placeholders in the skill instructions so the
	// agent knows the absolute file-system location of the skill directory.
	// Without this, scripts/, references/, and assets/ paths in SKILL.md
	// are unresolvable by read_file and bash tools.
	instructions := strings.ReplaceAll(s.Instructions, "${{SKILL_DIR}}", s.Path)

	scriptsInfo := ""
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		if len(names) > 0 {
			scriptsInfo = fmt.Sprintf("\nActive scripts:\n")
			for _, name := range names {
				scriptsInfo += fmt.Sprintf("  - %s (tool: skill_%s_%s)\n", name, s.Metadata.Name, sanitizeToolName(name))
			}
		}
	}

	return fmt.Sprintf(
		"Skill '%s' activated.\n\n"+
			"Skill Directory: %s\n"+
			"Scripts Directory: %s%s\n"+
			"Instructions:\n%s",
		s.Metadata.Name,
		s.Path,
		scriptsDir,
		scriptsInfo,
		instructions,
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

func sanitizeToolName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}
