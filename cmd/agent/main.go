package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentbootstrap "github.com/kubecrux/kubecrux/internal/agent/bootstrap"
	"go.uber.org/zap"
)

func main() {
	ctx := context.Background()
	application, err := agentbootstrap.New(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap kubecrux agent: %v\n", err)
		os.Exit(1)
	}

	go func() {
		if err := application.Run(); err != nil {
			application.Logger.Error("agent server exited with error", zap.Error(err))
			os.Exit(1)
		}
	}()

	application.Logger.Info("kubecrux agent started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		application.Logger.Error("agent graceful shutdown failed", zap.Error(err))
		os.Exit(1)
	}
}
