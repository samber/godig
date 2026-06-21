package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/godig/internal/mcpserver"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run godig as an MCP server",
		Args:  argsOrHelp(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			transport, _ := cmd.Flags().GetString("transport")
			addr, _ := cmd.Flags().GetString("addr")
			cacheTTL := viper.GetDuration("cache-ttl")
			cacheSize := viper.GetInt("cache-size")

			d, err := newDispatcher()
			if err != nil {
				return err
			}
			srv := mcpserver.New("godig", version, d, mcpserver.WithCache(cacheTTL, cacheSize))

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
	cmd.Flags().Duration("cache-ttl", 60*time.Minute, "in-memory result cache TTL (0 disables caching)")
	cmd.Flags().Int("cache-size", 100_000, "in-memory result cache capacity, in entries")
	_ = cmd.RegisterFlagCompletionFunc("transport", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"stdio", "http"}, cobra.ShellCompDirectiveNoFileComp
	})

	// Bind to viper so the cache can also be configured via env
	// (GODIG_CACHE_TTL, GODIG_CACHE_SIZE), consistent with the root flags.
	_ = viper.BindPFlag("cache-ttl", cmd.Flags().Lookup("cache-ttl"))
	_ = viper.BindPFlag("cache-size", cmd.Flags().Lookup("cache-size"))

	rootCmd.AddCommand(cmd)
}
