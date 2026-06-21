// Package logging configures the application's structured logger.
//
// All logs go to stderr: stdout is reserved for command output and for the MCP
// stdio transport, which would break if logs were interleaved with it.
package logging

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// levelOff disables logging (above any real level).
const levelOff = slog.Level(64)

// ParseLevel converts a level name to an slog.Level. "off"/"silent"/"none"
// disable logging entirely.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	case "off", "silent", "none":
		return levelOff, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (want debug|info|warn|error|off)", name)
	}
}

// Setup installs a stderr text logger at the given level as the slog default.
func Setup(level string) error {
	lvl, err := ParseLevel(level)
	if err != nil {
		return err
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
	return nil
}

// Transport wraps an http.RoundTripper to log every request/response at debug
// level (and failures at error level). Pass nil to wrap http.DefaultTransport.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &loggingTransport{base: base}
}

type loggingTransport struct {
	base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	slog.Debug("http request", "method", r.Method, "url", r.URL.String())

	resp, err := t.base.RoundTrip(r)
	dur := time.Since(start)
	if err != nil {
		slog.Error("http request failed", "method", r.Method, "url", r.URL.String(), "duration", dur, "err", err)
		return resp, err
	}
	slog.Debug("http response", "method", r.Method, "url", r.URL.String(), "status", resp.StatusCode, "duration", dur)
	return resp, err
}
