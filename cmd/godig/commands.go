package main

import (
	"fmt"
	"strings"

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
	// A second positional only makes sense alongside the first; catch catalog
	// misconfiguration at startup rather than producing a malformed command.
	if op.Arg2 != "" && op.Arg == "" {
		panic("spec: operation " + op.Name + " sets Arg2 without Arg")
	}

	use := op.Name
	var args []string
	argsRule := cobra.NoArgs
	if op.Arg != "" {
		use += " <" + op.Arg + ">"
		args = []string{op.Arg}
		argsRule = cobra.ExactArgs(1)
	}
	if op.Arg2 != "" {
		use += " <" + op.Arg2 + ">"
		args = append(args, op.Arg2)
		argsRule = cobra.ExactArgs(len(args))
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: op.Short,
		Long:  longHelp(op),
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

// longHelp augments the short description with a description of each positional
// argument. Cobra's usage line only shows argument names (<path> <symbol>), so
// without this the spec's ArgDesc/Arg2Desc — surfaced by the MCP tool schema —
// would be lost in the CLI help. Returns "" (cobra falls back to Short) when the
// operation takes no positional argument.
func longHelp(op spec.Operation) string {
	if op.Arg == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(op.Short)
	b.WriteString("\n\nArguments:\n")
	fmt.Fprintf(&b, "  %-8s %s\n", op.Arg, op.ArgDesc)
	if op.Arg2 != "" {
		fmt.Fprintf(&b, "  %-8s %s\n", op.Arg2, op.Arg2Desc)
	}
	return b.String()
}
