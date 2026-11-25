package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/opensoha/soha/internal/appgateway"
)

const usageText = `soha-app-gateway: standalone data-plane gateway for Workspace App Hosting

Configuration is supplied via SOHA_APP_GATEWAY_* environment variables.
`

func main() {
	os.Exit(run(os.Args[1:], os.LookupEnv))
}

func run(args []string, lookup func(string) (string, bool)) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := fmt.Fprint(os.Stdout, usageText); err != nil {
			return 1
		}
		return 0
	}
	if len(args) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "unexpected arguments: %v\n", args)
		return 2
	}
	cfg, err := appgateway.LoadConfigFromLookup(lookup)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	srv, err := appgateway.NewServer(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
