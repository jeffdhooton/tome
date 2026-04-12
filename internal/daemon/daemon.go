// Package daemon is the long-running tome process. It owns the Unix socket,
// the per-project BadgerDB stores, and the RPC dispatcher.
//
// One daemon per user. The CLI auto-spawns it on first call.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jeffdhooton/tome/internal/rpc"
)

// DefaultShutdownGrace is the time from SIGTERM to forceful close.
const DefaultShutdownGrace = 5 * time.Second

// Layout is the on-disk daemon layout under ~/.tome.
type Layout struct {
	Home       string // ~/.tome
	SocketPath string // ~/.tome/tomed.sock
	PIDPath    string // ~/.tome/tomed.pid
	LogPath    string // ~/.tome/tomed.log
}

// LayoutFor builds the layout from a tome home directory.
func LayoutFor(home string) Layout {
	return Layout{
		Home:       home,
		SocketPath: filepath.Join(home, "tomed.sock"),
		PIDPath:    filepath.Join(home, "tomed.pid"),
		LogPath:    filepath.Join(home, "tomed.log"),
	}
}

// Daemon is one running tomed process.
type Daemon struct {
	layout   Layout
	registry *Registry
	server   *rpc.Server

	mu       sync.Mutex
	listener net.Listener
}

// New constructs a Daemon. Call Run to begin serving.
func New(layout Layout) *Daemon {
	d := &Daemon{
		layout:   layout,
		registry: NewRegistry(),
		server:   rpc.NewServer(),
	}
	d.registerMethods()
	return d
}

// Run takes ownership of the process: writes the PID file, opens the socket,
// dispatches RPC calls until ctx is cancelled or SIGTERM/SIGINT arrives.
func (d *Daemon) Run(ctx context.Context) error {
	raiseNOFILE()

	if err := os.MkdirAll(d.layout.Home, 0o755); err != nil {
		return fmt.Errorf("ensure home: %w", err)
	}

	if alive, pid := d.aliveDaemonPID(); alive {
		return fmt.Errorf("tome daemon already running (pid %d, socket %s)", pid, d.layout.SocketPath)
	}

	// Remove stale socket from a previous crash.
	if err := os.Remove(d.layout.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", d.layout.SocketPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", d.layout.SocketPath, err)
	}
	if err := os.Chmod(d.layout.SocketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	d.mu.Lock()
	d.listener = ln
	d.mu.Unlock()

	if err := os.WriteFile(d.layout.PIDPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		_ = ln.Close()
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(d.layout.PIDPath)
	defer os.Remove(d.layout.SocketPath)
	defer d.registry.CloseAll()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-runCtx.Done():
		}
	}()

	serveErr := d.server.Serve(runCtx, ln)
	if serveErr != nil && !errors.Is(serveErr, net.ErrClosed) {
		return serveErr
	}
	return nil
}

func (d *Daemon) aliveDaemonPID() (bool, int) {
	return AliveDaemon(d.layout)
}

// AliveDaemon checks whether a daemon is currently running.
func AliveDaemon(layout Layout) (bool, int) {
	pidBytes, err := os.ReadFile(layout.PIDPath)
	if err != nil {
		if pingSocket(layout.SocketPath) {
			return true, 0
		}
		return false, 0
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(pidBytes)))
	if err != nil {
		return false, 0
	}
	if !processAlive(pid) {
		return false, 0
	}
	if !pingSocket(layout.SocketPath) {
		return false, 0
	}
	return true, pid
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func pingSocket(path string) bool {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
}
