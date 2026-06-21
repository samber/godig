package main

import (
	"fmt"
	"log/slog"

	"github.com/samber/godig/internal/mcpserver"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run godig as an MCP server",
		Args:  argsOrHelp(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			transport, _ := cmd.Flags().GetString("transport")
			addr, _ := cmd.Flags().GetString("addr")

			d, err := newDispatcher()
			if err != nil {
				return err
			}
			srv := mcpserver.New("godig", version, d)

			switch transport {
			case "stdio":
				slog.Info("starting MCP server", "transport", "stdio")
				return mcpserver.ServeStdio(srv)
			case "http":
				slog.Info("starting MCP server", "transport", "http", "url", "http://"+addr+"/mcp")
				return mcpserver.ServeHTTP(srv, addr)
			default:
				return fmt.Errorf("unknown transport %q (want stdio or http)", transport)
			}
		},
	}

	cmd.Flags().StringP("transport", "t", "stdio", "MCP transport: stdio or http")
	cmd.Flags().String("addr", ":8080", "listen address for the http transport")
	_ = cmd.RegisterFlagCompletionFunc("transport", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"stdio", "http"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(cmd)
}
