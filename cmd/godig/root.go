package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	pkggodev "github.com/samber/go-pkggodev-client"
	"github.com/samber/godig/internal/dispatch"
	"github.com/samber/godig/internal/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd is the base command for godig.
var rootCmd = &cobra.Command{
	Use:           "godig",
	Short:         "Explore Go packages and modules from pkg.go.dev",
	Long:          "godig is a CLI and MCP server for the pkg.go.dev API: search packages, read docs, list versions, symbols, importers and vulnerabilities.",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return logging.Setup(viper.GetString("log-level"))
	},
}

// Execute runs the root command. Called by main. It is split from run so the
// signal-context cleanup (defer) runs before the process exits.
func Execute() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

// run executes the root command and returns the process exit code.
func run() int {
	// Cancel in-flight work (e.g. an HTTP request to pkg.go.dev) on Ctrl-C or
	// SIGTERM instead of blocking until the request timeout elapses. The signal
	// set is platform-specific (see signals_unix.go / signals_windows.go).
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer stop()

	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	// Invalid args/flags or a group invoked without a subcommand already printed
	// the full help (see argsOrHelp / the group RunE). It is a usage error, so
	// exit 2 (Unix convention) — not 0, which would hide the mistake from scripts.
	if errors.Is(err, errHelpShown) {
		return 2
	}
	// A dispatcher validation failure (e.g. a non-positive --limit) is likewise a
	// usage error: print the message and exit 2, consistent with bad flags above.
	var usage *dispatch.UsageError
	if errors.As(err, &usage) {
		fmt.Fprintln(os.Stderr, "godig:", err)
		return 2
	}
	fmt.Fprintln(os.Stderr, "godig:", err)
	return 1
}

func init() {
	// On a flag error, show the command's full help (like --help) instead of a terse message.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		_ = cmd.Help()
		return errHelpShown
	})

	pf := rootCmd.PersistentFlags()
	pf.String("base-url", pkggodev.DefaultBaseURL, "pkg.go.dev API base URL")
	pf.Duration("timeout", 30*time.Second, "HTTP request timeout")
	pf.StringP("output", "o", "table", "output format: table|json|raw|md")
	pf.String("log-level", "error", "log level: debug|info|warn|error|off (logs go to stderr)")

	viper.SetEnvPrefix("GODIG")
	// Map dashed flag names to underscored env vars (e.g. base-url -> GODIG_BASE_URL).
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	_ = viper.BindPFlag("base-url", pf.Lookup("base-url"))
	_ = viper.BindPFlag("timeout", pf.Lookup("timeout"))
	_ = viper.BindPFlag("output", pf.Lookup("output"))
	_ = viper.BindPFlag("log-level", pf.Lookup("log-level"))
}

// outputFormat returns the resolved output format.
func outputFormat() string { return viper.GetString("output") }

// newDispatcher builds a dispatcher backed by a configured pkg.go.dev client.
func newDispatcher() (*dispatch.Dispatcher, error) {
	baseURL := viper.GetString("base-url")
	timeout := viper.GetDuration("timeout")
	slog.Debug("building pkg.go.dev client", "base_url", baseURL, "timeout", timeout)

	c, err := pkggodev.New(
		pkggodev.WithBaseURL(baseURL),
		pkggodev.WithHTTPClient(&http.Client{
			Timeout:   timeout,
			Transport: logging.Transport(nil),
		}),
		pkggodev.WithUserAgent("samber/godig/"+version),
	)
	if err != nil {
		return nil, err
	}
	return dispatch.New(c), nil
}
