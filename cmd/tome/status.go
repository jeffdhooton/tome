package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List indexed projects and daemon state",
		RunE: func(cmd *cobra.Command, args []string) error {
			pretty, _ := cmd.Flags().GetBool("pretty")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var result json.RawMessage
			if err := callDaemon(ctx, "status", struct{}{}, &result); err != nil {
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
