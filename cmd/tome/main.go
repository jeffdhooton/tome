// Command tome is the schema-awareness CLI for AI agents.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Version is set by ldflags during release builds.
var Version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "tome",
		Short: "Schema awareness daemon for AI agents",
		Long: `tome is a schema and data-contract awareness daemon for AI agents.
It pre-indexes database schemas, API response shapes, and enum values, then
serves them as millisecond-latency MCP queries — replacing the 3-6 file reads
an agent does every time it needs to answer "what columns does this table have."`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().String("project", "", "project root (defaults to cwd)")
	root.PersistentFlags().Bool("pretty", false, "pretty-print JSON output for human reading")

	root.AddCommand(versionCmd())
	root.AddCommand(initCmd())
	root.AddCommand(describeCmd())
	root.AddCommand(relationsCmd())
	root.AddCommand(searchCmd())
	root.AddCommand(enumsCmd())
	root.AddCommand(refreshCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(startCmd())
	root.AddCommand(stopCmd())
	root.AddCommand(mcpCmd())
	root.AddCommand(setupCmd())
	root.AddCommand(doctorCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tome:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print tome version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("tome", Version)
			return nil
		},
	}
}

// resolveProject returns the absolute project root selected by --project or cwd.
func resolveProject(cmd *cobra.Command) (string, error) {
	override, _ := cmd.Flags().GetString("project")
	if override != "" {
		return filepath.Abs(override)
	}
	return os.Getwd()
}

// tomeHome returns ~/.tome, creating it if missing.
func tomeHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	dir := filepath.Join(home, ".tome")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create tome home: %w", err)
	}
	return dir, nil
}
