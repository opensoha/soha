package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kubecrux/kubecrux/internal/bootstrap"
	"go.uber.org/zap"
)

func main() {
	ctx := context.Background()
	application, err := bootstrap.New(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap kubecrux api: %v\n", err)
		os.Exit(1)
	}

	go func() {
		if err := application.Run(); err != nil {
			application.Logger.Error("server exited with error", zap.Error(err))
			os.Exit(1)
		}
	}()

	application.Logger.Info("kubecrux api started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		application.Logger.Error("graceful shutdown failed", zap.Error(err))
		os.Exit(1)
	}
}
