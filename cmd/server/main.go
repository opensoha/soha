package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opensoha/soha/internal/bootstrap"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runServer(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runServer(ctx context.Context) error {
	application, err := bootstrap.New(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap soha api: %w", err)
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- application.Run()
	}()

	application.Logger.Info("soha api started")

	var runFailure error
	select {
	case <-ctx.Done():
	case err := <-runErr:
		if err != nil {
			application.Logger.Error("server exited with error", zap.Error(err))
			runFailure = fmt.Errorf("run soha api: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		application.Logger.Error("graceful shutdown failed", zap.Error(err))
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return runFailure
}
