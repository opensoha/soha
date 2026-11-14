package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/opensoha/soha/internal/networkproxyruntime"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--kernel-preflight-help" || os.Args[1] == "--help") {
		if _, err := fmt.Fprint(os.Stdout, networkproxyruntime.KernelPreflightHelp); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--kernel-preflight" {
		path := ""
		if len(os.Args) == 3 {
			path = os.Args[2]
		}
		report := networkproxyruntime.RunKernelPreflight(context.Background(), path)
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil || !report.Compatible {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--adapter-reference" {
		if err := networkproxyruntime.ServeReferenceAdapter(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	config, err := networkproxyruntime.LoadConfig()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runner, err := networkproxyruntime.NewRunner(config)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
