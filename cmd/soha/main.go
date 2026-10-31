package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/opensoha/soha-cli/internal/sohacli"
)

func main() {
	os.Exit(sohacli.Run(context.Background(), os.Args[1:], sohacli.Runtime{
		DeviceLoginContext: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		},
		TerminalContext: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		},
		StreamingContext: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		},
	}))
}
