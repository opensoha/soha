package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opensoha/soha/internal/networkgateway"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := networkgateway.LoadWorkloadConfig()
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		err = networkgateway.RunWorkload(ctx, config, os.Getenv("SOHA_NETWORK_WORKLOAD_API_URL"), os.Getenv("SOHA_NETWORK_WORKLOAD_TOKEN_FILE"), os.Getenv("SOHA_NETWORK_WORKLOAD_BINDING_FILE"), logger)
	}
	if err != nil {
		logger.Error("workload stopped", "event", "network_workload.failed")
		os.Exit(1)
	}
}
