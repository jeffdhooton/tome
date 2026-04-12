package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/daemon"
)

func refreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Re-introspect the database and update the cached schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd)
			if err != nil {
				return err
			}
			pretty, _ := cmd.Flags().GetBool("pretty")

			params := struct {
				Project string `json:"project"`
			}{Project: project}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var result daemon.InitResult
			if err := callDaemon(ctx, "refresh", params, &result); err != nil {
				return err
			}

			if pretty {
				b, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(b))
			} else {
				b, _ := json.Marshal(result)
				fmt.Println(string(b))
			}
			fmt.Fprintf(os.Stderr, "tome: refreshed %d tables in %dms\n",
				result.TableCount, result.ElapsedMs)
			return nil
		},
	}
}
