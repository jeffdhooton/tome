package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jeffdhooton/tome/internal/mcp"
	"github.com/jeffdhooton/tome/internal/rpc"
)

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run as an MCP stdio server (launched by Claude Code)",
		Long: `Speaks the Model Context Protocol on stdin/stdout so Claude Code
can call tome queries as first-class tools. The daemon is auto-spawned on first
call if it isn't already running.

Not meant to be run interactively. Configure via 'tome setup'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
			defer signal.Stop(sigCh)
			go func() {
				<-sigCh
				cancel()
			}()

			dial := func() (mcp.Dialer, error) {
				c, err := dialDaemon()
				if err != nil {
					return nil, err
				}
				return &mcpDialer{c: c}, nil
			}
			srv := mcp.New(dial)
			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}
}

type mcpDialer struct {
	c *rpc.Client
}

func (d *mcpDialer) Call(ctx context.Context, method string, params, out any) error {
	return d.c.Call(ctx, method, params, out)
}

func (d *mcpDialer) Close() error {
	return d.c.Close()
}
