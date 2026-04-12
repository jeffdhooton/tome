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

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Connect to a database and index its schema",
		Long: `Connect to a local database, introspect its schema (tables, columns,
indexes, foreign keys, enums), and cache the results for instant querying.

The DSN can be provided with --dsn or auto-detected from .env with --detect-env.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd)
			if err != nil {
				return err
			}
			dsn, _ := cmd.Flags().GetString("dsn")
			detectEnv, _ := cmd.Flags().GetBool("detect-env")
			pretty, _ := cmd.Flags().GetBool("pretty")

			params := daemon.InitParams{
				Project:   project,
				DSN:       dsn,
				DetectEnv: detectEnv,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var result daemon.InitResult
			if err := callDaemon(ctx, "init", params, &result); err != nil {
				return err
			}

			if pretty {
				b, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(b))
			} else {
				b, _ := json.Marshal(result)
				fmt.Println(string(b))
			}
			fmt.Fprintf(os.Stderr, "tome: indexed %d tables from %s (%s) in %dms\n",
				result.TableCount, result.DBName, result.DBType, result.ElapsedMs)
			return nil
		},
	}
	cmd.Flags().String("dsn", "", `database connection string (e.g. "mysql://user:pass@localhost/mydb")`)
	cmd.Flags().Bool("detect-env", false, "auto-detect DSN from .env file")
	return cmd
}
