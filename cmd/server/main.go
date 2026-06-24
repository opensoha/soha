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

	application, err := bootstrap.New(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap soha api: %v\n", err)
		return 1
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- application.Run()
	}()

	application.Logger.Info("soha api started")

	exitCode := 0
	select {
	case <-ctx.Done():
		stop()
	case err := <-runErr:
		if err != nil {
			application.Logger.Error("server exited with error", zap.Error(err))
			exitCode = 1
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		application.Logger.Error("graceful shutdown failed", zap.Error(err))
		return 1
	}

	return exitCode
}
