package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/redaction"
	"github.com/opensoha/soha/internal/radiusadapter"
)

func main() { os.Exit(run()) }

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := radiusadapter.LoadConfig()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
		return 1
	}
	client, err := radiusadapter.NewHTTPClient(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
		return 1
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
		return 1
	}
	journal, err := radiusadapter.OpenJournal(cfg.JournalFile)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
		return 1
	}
	adapter, err := radiusadapter.New(cfg, client, schemas, journal, radiusadapter.RadclientExecutor(cfg), logger)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := adapter.Run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
		return 1
	}
	return 0
}
