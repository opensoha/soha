package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opensoha/soha/internal/ingestapp"
	"github.com/opensoha/soha/internal/platform/redaction"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	application, err := ingestapp.New(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	runErr := make(chan error, 1)
	go func() { runErr <- application.Run() }()
	var failure error
	select {
	case <-ctx.Done():
	case err := <-runErr:
		if err != nil {
			application.Logger.Error("network ingest exited with error", zap.String("event", "network_ingest.run.failed"), zap.String("error", redaction.LogText(err.Error(), 2048)))
			failure = err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if failure != nil {
		return 1
	}
	return 0
}
