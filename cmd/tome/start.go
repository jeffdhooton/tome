package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/daemon"
)

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the tome daemon",
		Long: `Start the long-running tomed daemon. With --foreground, tomed runs in
the calling shell. The CLI auto-spawns the daemon on first query, so manual
start is only needed for debugging.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			foreground, _ := cmd.Flags().GetBool("foreground")
			home, err := tomeHome()
			if err != nil {
				return err
			}
			layout := daemon.LayoutFor(home)

			if foreground {
				d := daemon.New(layout)
				return d.Run(context.Background())
			}

			if alive, pid := daemon.AliveDaemon(layout); alive {
				fmt.Fprintf(os.Stderr, "tome: daemon already running (pid %d)\n", pid)
				return nil
			}
			if err := spawnDaemon(); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "tome: daemon spawned")
			return nil
		},
	}
	cmd.Flags().Bool("foreground", false, "run the daemon in the foreground (do not detach)")
	return cmd
}
