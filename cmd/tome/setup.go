package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/setup"
)

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install Claude Code integration (SKILL.md + MCP server)",
		Long: `Installs the tome skill file and registers tome as an MCP server
with Claude Code. Run this after installing or upgrading tome.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			force, _ := cmd.Flags().GetBool("force")
			tomeBin, _ := cmd.Flags().GetString("tome-binary")

			res, err := setup.Install(setup.Options{
				TomeBinary: tomeBin,
				DryRun:     dryRun,
				Force:      force,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Skill:  %s (%s)\n", res.SkillPath, res.SkillAction)
			fmt.Fprintf(os.Stderr, "MCP:    %s\n", res.MCPAction)
			if res.MCPAction == "manual" {
				fmt.Fprintf(os.Stderr, "\nClaude CLI not found. Run manually:\n  %s\n", res.MCPCommand)
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "show planned changes without touching disk")
	cmd.Flags().Bool("force", false, "overwrite existing skill and re-register MCP server")
	cmd.Flags().String("tome-binary", "", "path to the tome binary to register (defaults to current binary)")
	return cmd
}
