// Package setup installs the tome Claude Code integration: writes the
// embedded SKILL.md and registers tome as an MCP server via `claude mcp add`.
package setup

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed SKILL.md
var embeddedSkill []byte

// Options controls the install behavior.
type Options struct {
	TomeBinary string
	DryRun     bool
	Force      bool
}

// Result summarizes what Install did.
type Result struct {
	SkillPath      string
	SkillAction    string // "written" | "unchanged" | "dry-run"
	MCPAction      string // "registered" | "replaced" | "unchanged" | "dry-run" | "manual"
	MCPCommand     string
	MCPBinary      string
	ClaudeCLIFound bool
}

// Install performs the full Claude Code integration.
func Install(opts Options) (*Result, error) {
	claudeHome, err := claudeHomeDir()
	if err != nil {
		return nil, err
	}
	res := &Result{
		SkillPath: filepath.Join(claudeHome, "skills", "tome", "SKILL.md"),
	}

	if err := installSkill(opts, res); err != nil {
		return res, fmt.Errorf("install skill: %w", err)
	}
	if err := installMCPServer(opts, res); err != nil {
		return res, fmt.Errorf("install mcp server: %w", err)
	}
	return res, nil
}

func claudeHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create claude home: %w", err)
	}
	return dir, nil
}

func installSkill(opts Options, res *Result) error {
	dir := filepath.Dir(res.SkillPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}

	existing, readErr := os.ReadFile(res.SkillPath)
	if readErr == nil && bytesEqual(existing, embeddedSkill) && !opts.Force {
		res.SkillAction = "unchanged"
		return nil
	}

	if opts.DryRun {
		res.SkillAction = "dry-run"
		return nil
	}

	tmp, err := os.CreateTemp(dir, "SKILL.md.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(embeddedSkill); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, res.SkillPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	res.SkillAction = "written"
	return nil
}

func installMCPServer(opts Options, res *Result) error {
	bin := opts.TomeBinary
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate tome binary: %w", err)
		}
		bin = exe
	}
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	res.MCPBinary = bin

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		res.ClaudeCLIFound = false
		res.MCPCommand = fmt.Sprintf("claude mcp add --scope user --transport stdio tome -- %q mcp", bin)
		res.MCPAction = "manual"
		return nil
	}
	res.ClaudeCLIFound = true
	res.MCPCommand = fmt.Sprintf("%s mcp add --scope user --transport stdio tome -- %q mcp", claudeBin, bin)

	current, currentErr := runClaudeMCP(claudeBin, "get", "tome")
	hasTome := currentErr == nil && len(current) > 0
	commandMatches := hasTome && strings.Contains(current, bin) && strings.Contains(current, " mcp")

	if commandMatches && !opts.Force {
		res.MCPAction = "unchanged"
		return nil
	}

	if opts.DryRun {
		if hasTome {
			res.MCPAction = "replaced (dry-run)"
		} else {
			res.MCPAction = "registered (dry-run)"
		}
		return nil
	}

	if hasTome {
		if _, err := runClaudeMCP(claudeBin, "remove", "tome"); err != nil {
			return fmt.Errorf("remove existing tome entry: %w", err)
		}
	}

	cmd := exec.Command(claudeBin, "mcp", "add", "--scope", "user", "--transport", "stdio", "tome", "--", bin, "mcp")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude mcp add: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if hasTome {
		res.MCPAction = "replaced"
	} else {
		res.MCPAction = "registered"
	}
	return nil
}

func runClaudeMCP(claudeBin string, args ...string) (string, error) {
	cmd := exec.Command(claudeBin, append([]string{"mcp"}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
