package main

import (
	"github.com/samber/godig/internal/spec"
	"github.com/spf13/cobra"
)

// init builds the command tree from the hand-written catalog (internal/spec):
// top-level operations become root commands, and grouped operations become
// subcommands of a parent command (e.g. `godig package info`). Each leaf is
// wired to the go-pkggodev-client via runOperation.
func init() {
	parents := map[string]*cobra.Command{}

	for _, op := range spec.Operations {
		leaf := buildCommand(op)
		if op.Group == "" {
			rootCmd.AddCommand(leaf)
			continue
		}
		parent, ok := parents[op.Group]
		if !ok {
			group := op.Group
			parent = &cobra.Command{
				Use:   group,
				Short: spec.GroupShort(group),
				// Running the parent (or an unknown subcommand) shows its help.
				RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
			}
			parents[group] = parent
			rootCmd.AddCommand(parent)
		}
		parent.AddCommand(leaf)
	}
}

func buildCommand(op spec.Operation) *cobra.Command {
	use := op.Name
	var args []string
	argsRule := cobra.NoArgs
	if op.Arg != "" {
		use += " <" + op.Arg + ">"
		args = []string{op.Arg}
		argsRule = cobra.ExactArgs(1)
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: op.Short,
		Args:  argsOrHelp(argsRule),
		RunE: func(cmd *cobra.Command, posArgs []string) error {
			return runOperation(cmd, op.Key(), args, posArgs)
		},
	}

	for _, p := range op.Params {
		switch p.Type {
		case spec.Int:
			cmd.Flags().Int(p.Name, 0, p.Desc)
		case spec.Bool:
			cmd.Flags().Bool(p.Name, false, p.Desc)
		case spec.String:
			cmd.Flags().String(p.Name, "", p.Desc)
		}
	}

	return cmd
}
