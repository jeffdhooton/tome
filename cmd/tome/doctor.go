package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/doctor"
)

func doctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check tome environment and integration health",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")

			home, _ := tomeHome()
			checks := doctor.Run(home)

			if jsonOut {
				out, err := doctor.FormatJSON(checks)
				if err != nil {
					return err
				}
				fmt.Println(out)
			} else {
				fmt.Print(doctor.FormatPretty(checks))
			}

			if doctor.HasFailures(checks) {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}
