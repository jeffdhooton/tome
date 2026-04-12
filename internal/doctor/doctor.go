// Package doctor provides read-only environment diagnostics for tome.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeffdhooton/tome/internal/daemon"
)

// Check is one diagnostic check.
type Check struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Status   string `json:"status"` // "ok" | "warn" | "fail" | "skip"
	Message  string `json:"message"`
}

// Run executes all diagnostic checks and returns the results.
func Run(tomeHome string) []Check {
	var checks []Check

	// Environment checks.
	checks = append(checks, checkBinary())
	checks = append(checks, checkHome(tomeHome))

	// Daemon checks.
	checks = append(checks, checkDaemon(tomeHome))

	// Claude Code integration.
	checks = append(checks, checkClaudeCLI())
	checks = append(checks, checkMCPRegistered())
	checks = append(checks, checkSkillInstalled())

	return checks
}

// HasFailures returns true if any check failed.
func HasFailures(checks []Check) bool {
	for _, c := range checks {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}

// FormatPretty returns a human-readable checklist.
func FormatPretty(checks []Check) string {
	var b strings.Builder
	category := ""
	for _, c := range checks {
		if c.Category != category {
			if category != "" {
				b.WriteString("\n")
			}
			category = c.Category
			fmt.Fprintf(&b, "%s:\n", category)
		}
		icon := "?"
		switch c.Status {
		case "ok":
			icon = "ok"
		case "warn":
			icon = "!!"
		case "fail":
			icon = "FAIL"
		case "skip":
			icon = "--"
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", icon, c.Name, c.Message)
	}
	return b.String()
}

func checkBinary() Check {
	exe, err := os.Executable()
	if err != nil {
		return Check{Category: "Environment", Name: "Binary", Status: "fail", Message: err.Error()}
	}
	return Check{Category: "Environment", Name: "Binary", Status: "ok", Message: exe}
}

func checkHome(tomeHome string) Check {
	if tomeHome == "" {
		return Check{Category: "Environment", Name: "Home directory", Status: "fail", Message: "could not determine tome home"}
	}
	info, err := os.Stat(tomeHome)
	if err != nil {
		return Check{Category: "Environment", Name: "Home directory", Status: "warn", Message: tomeHome + " does not exist (will be created on first use)"}
	}
	if !info.IsDir() {
		return Check{Category: "Environment", Name: "Home directory", Status: "fail", Message: tomeHome + " is not a directory"}
	}
	return Check{Category: "Environment", Name: "Home directory", Status: "ok", Message: tomeHome}
}

func checkDaemon(tomeHome string) Check {
	if tomeHome == "" {
		return Check{Category: "Daemon", Name: "Running", Status: "skip", Message: "no home directory"}
	}
	layout := daemon.LayoutFor(tomeHome)
	alive, pid := daemon.AliveDaemon(layout)
	if alive {
		return Check{Category: "Daemon", Name: "Running", Status: "ok", Message: fmt.Sprintf("pid %d", pid)}
	}
	return Check{Category: "Daemon", Name: "Running", Status: "ok", Message: "idle (auto-spawns on first MCP tool call)"}
}

func checkClaudeCLI() Check {
	path, err := exec.LookPath("claude")
	if err != nil {
		return Check{Category: "Claude Code", Name: "CLI", Status: "warn", Message: "claude not found on PATH"}
	}
	return Check{Category: "Claude Code", Name: "CLI", Status: "ok", Message: path}
}

func checkMCPRegistered() Check {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return Check{Category: "Claude Code", Name: "MCP registered", Status: "skip", Message: "claude CLI not available"}
	}
	cmd := exec.Command(claudeBin, "mcp", "get", "tome")
	out, err := cmd.CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return Check{Category: "Claude Code", Name: "MCP registered", Status: "warn", Message: "tome not registered — run 'tome setup'"}
	}
	return Check{Category: "Claude Code", Name: "MCP registered", Status: "ok", Message: "registered"}
}

func checkSkillInstalled() Check {
	home, err := os.UserHomeDir()
	if err != nil {
		return Check{Category: "Claude Code", Name: "Skill installed", Status: "skip", Message: "cannot find home dir"}
	}
	skillPath := filepath.Join(home, ".claude", "skills", "tome", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		return Check{Category: "Claude Code", Name: "Skill installed", Status: "warn", Message: "not installed — run 'tome setup'"}
	}
	return Check{Category: "Claude Code", Name: "Skill installed", Status: "ok", Message: skillPath}
}

// FormatJSON returns JSON output.
func FormatJSON(checks []Check) (string, error) {
	b, err := json.MarshalIndent(checks, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
