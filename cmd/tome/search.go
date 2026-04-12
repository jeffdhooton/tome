package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/daemon"
)

func searchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <term>",
		Short: "Search for tables or columns by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd)
			if err != nil {
				return err
			}
			pretty, _ := cmd.Flags().GetBool("pretty")

			params := daemon.SearchParams{
				Project: project,
				Query:   args[0],
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var result json.RawMessage
			if err := callDaemon(ctx, "search", params, &result); err != nil {
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
