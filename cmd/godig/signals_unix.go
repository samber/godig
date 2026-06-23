//go:build !windows

package main

import (
	"os"
	"syscall"
)

// shutdownSignals are the OS signals that trigger graceful cancellation.
// On Unix-like systems this includes SIGTERM (sent by init systems, Docker,
// Kubernetes, etc.) in addition to SIGINT (Ctrl-C).
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
