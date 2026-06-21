package main

import (
	"context"
	"log/slog"

	"github.com/samber/godig/internal/render"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// runOperation is the shared entry point for every operation command. It builds
// the argument map from positional path args plus changed local flags, invokes
// the operation through the dispatcher, and renders the response.
func runOperation(cmd *cobra.Command, name string, pathParams, args []string) error {
	m := map[string]any{}
	for i, p := range pathParams {
		if i < len(args) {
			m[p] = args[i]
		}
	}

	cmd.Flags().Visit(func(f *pflag.Flag) {
		switch f.Value.Type() {
		case "bool":
			b, _ := cmd.Flags().GetBool(f.Name)
			m[f.Name] = b
		case "int":
			n, _ := cmd.Flags().GetInt(f.Name)
			m[f.Name] = n
		default:
			m[f.Name] = f.Value.String()
		}
	})

	slog.Debug("running command", "command", name, "args", m)

	d, err := newDispatcher()
	if err != nil {
		return err
	}
	out, err := d.Invoke(context.Background(), name, m)
	if err != nil {
		slog.Error("command failed", "command", name, "err", err)
		return err
	}
	return render.Write(cmd.OutOrStdout(), out, outputFormat())
}
