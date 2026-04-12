package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/jeffdhooton/tome/internal/daemon"
	"github.com/jeffdhooton/tome/internal/rpc"
)

// dialDaemon opens a client connection to the running daemon, auto-spawning
// it if it isn't already up.
func dialDaemon() (*rpc.Client, error) {
	home, err := tomeHome()
	if err != nil {
		return nil, err
	}
	layout := daemon.LayoutFor(home)

	if alive, _ := daemon.AliveDaemon(layout); !alive {
		if err := spawnDaemon(); err != nil {
			return nil, fmt.Errorf("auto-spawn daemon: %w", err)
		}
		if err := waitForSocket(layout.SocketPath, 2*time.Second); err != nil {
			return nil, fmt.Errorf("daemon did not come up: %w", err)
		}
	}

	c, err := rpc.Dial(layout.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("dial daemon: %w", err)
	}
	return c, nil
}

// spawnDaemon forks the current tome binary as a detached background process.
func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	home, err := tomeHome()
	if err != nil {
		return err
	}
	layout := daemon.LayoutFor(home)

	logFile, err := os.OpenFile(layout.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "start", "--foreground")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start child: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release child: %w", err)
	}
	return nil
}

// waitForSocket polls until the daemon is accepting connections.
func waitForSocket(socketPath string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	delay := 10 * time.Millisecond
	for {
		if pingSocket(socketPath) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for daemon socket")
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
}

func pingSocket(path string) bool {
	c, err := rpc.Dial(path)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// callDaemon is a one-shot helper: dial, call, close.
func callDaemon(ctx context.Context, method string, params, out any) error {
	c, err := dialDaemon()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(ctx, method, params, out)
}
