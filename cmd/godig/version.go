package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata, injected via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	rootCmd.Version = version
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version, commit and build date",
		Args:  argsOrHelp(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "godig %s (commit %s, built %s)\n", version, commit, date)
			return err
		},
	})
}
