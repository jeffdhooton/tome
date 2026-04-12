package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/daemon"
)

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running tome daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := tomeHome()
			if err != nil {
				return err
			}
			layout := daemon.LayoutFor(home)

			alive, pid := daemon.AliveDaemon(layout)
			if !alive {
				fmt.Fprintln(os.Stderr, "tome: no daemon running")
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			if err := callDaemon(ctx, "shutdown", nil, nil); err != nil {
				if pid > 0 {
					_ = syscall.Kill(pid, syscall.SIGTERM)
				}
			}

			deadline := time.Now().Add(daemon.DefaultShutdownGrace)
			for time.Now().Before(deadline) {
				if alive, _ := daemon.AliveDaemon(layout); !alive {
					fmt.Fprintln(os.Stderr, "tome: daemon stopped")
					return nil
				}
				time.Sleep(50 * time.Millisecond)
			}

			if pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			fmt.Fprintln(os.Stderr, "tome: daemon force-killed after grace period")
			return nil
		},
	}
}
