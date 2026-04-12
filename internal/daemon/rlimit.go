package daemon

import (
	"fmt"
	"os"
	"syscall"
)

// raiseNOFILE bumps the soft NOFILE limit up to the hard limit.
func raiseNOFILE() {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		fmt.Fprintf(os.Stderr, "tome: getrlimit NOFILE: %v\n", err)
		return
	}
	if lim.Cur >= lim.Max {
		return
	}
	lim.Cur = lim.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		fmt.Fprintf(os.Stderr, "tome: setrlimit NOFILE: %v\n", err)
	}
}
