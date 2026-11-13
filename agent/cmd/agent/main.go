package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentbootstrap "github.com/opensoha/soha-agent/internal/agent/bootstrap"
	"github.com/opensoha/soha-agent/internal/agent/buildinfo"
	"github.com/opensoha/soha-agent/internal/agent/dockerruntime"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "volume-reader" {
		if len(args) != 1 {
			return 2
		}
		return dockerruntime.VolumeReaderMain(os.Stdin, os.Stdout)
	}
	if shouldPrintVersion(args) {
		fmt.Println(buildinfo.Human())
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := agentbootstrap.New(ctx)
	if err != nil {
		_ = json.NewEncoder(os.Stderr).Encode(struct {
			Timestamp string `json:"timestamp"`
			Level     string `json:"level"`
			Component string `json:"component"`
			Service   string `json:"service"`
			Event     string `json:"event"`
			Message   string `json:"message"`
			ErrorType string `json:"error_type"`
		}{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "error",
			Component: "bootstrap",
			Service:   "soha-agent",
			Event:     "agent.bootstrap.failed",
			Message:   "soha agent bootstrap failed",
			ErrorType: fmt.Sprintf("%T", err),
		})
		return 1
	}
	lifecycleLogger := application.Logger.Named("lifecycle")

	runErr := make(chan error, 1)
	go func() {
		runErr <- application.Run()
	}()

	lifecycleLogger.Info("soha agent started", zap.String("event", "agent.started"))

	exitCode := 0
	select {
	case <-ctx.Done():
		stop()
	case err := <-runErr:
		if err != nil {
			lifecycleLogger.Error("agent server exited with error",
				zap.String("event", "agent.server.failed"),
				zap.String("error_type", fmt.Sprintf("%T", err)),
			)
			exitCode = 1
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		lifecycleLogger.Error("agent graceful shutdown failed",
			zap.String("event", "agent.shutdown.failed"),
			zap.String("error_type", fmt.Sprintf("%T", err)),
		)
		return 1
	}

	return exitCode
}

func shouldPrintVersion(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "--version", "-version", "-v":
		return true
	default:
		return false
	}
}
