//go:build windows

package main

import "os"

// shutdownSignals are the OS signals that trigger graceful cancellation.
// Windows does not deliver SIGTERM to console applications; os.Interrupt
// (Ctrl-C / Ctrl-Break) is the only portable shutdown signal there.
var shutdownSignals = []os.Signal{os.Interrupt}
