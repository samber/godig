package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// errHelpShown is a sentinel returned after a command has already printed its
// full help in response to invalid arguments or flags. Execute treats it as a
// clean exit (same outcome as running the command with --help).
var errHelpShown = errors.New("help shown")

// argsOrHelp wraps a positional-args validator so that invalid arguments print
// the command's full help (same output as --help) instead of a terse error.
func argsOrHelp(inner cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if inner == nil {
			return nil
		}
		if err := inner(cmd, args); err != nil {
			_ = cmd.Help()
			return errHelpShown
		}
		return nil
	}
}
