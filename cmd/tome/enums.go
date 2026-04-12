package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/daemon"
)

func enumsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enums [table.column]",
		Short: "List enum values for database columns",
		Long: `List the allowed values for enum/set database columns.

With no arguments, lists all enums for the indexed database.
With table.column, shows values for that specific column.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd)
			if err != nil {
				return err
			}
			pretty, _ := cmd.Flags().GetBool("pretty")

			params := daemon.EnumsParams{
				Project: project,
			}
			if len(args) > 0 {
				parts := strings.SplitN(args[0], ".", 2)
				params.Table = parts[0]
				if len(parts) == 2 {
					params.Column = parts[1]
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var result json.RawMessage
			if err := callDaemon(ctx, "enums", params, &result); err != nil {
				return err
			}

			if pretty {
				var v any
				_ = json.Unmarshal(result, &v)
				b, _ := json.MarshalIndent(v, "", "  ")
				fmt.Println(string(b))
			} else {
				fmt.Println(string(result))
			}
			return nil
		},
	}
}
