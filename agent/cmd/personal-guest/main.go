package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/opensoha/soha-agent/internal/agent/bootstrap/personalguest"
)

func main() {
	path := flag.String("config", "", "absolute private personal guest configuration file")
	dataPlan := flag.String("initialize-data", "", "operator-owned Linux data initialization plan; requires root")
	dataRef := flag.String("reconcile-data", "", "remount initialized managed data; cannot format; requires root")
	tools := flag.String("tools", "", "root-owned fixed runtime tools manifest")
	runtimeRef := flag.String("runtime", "", "launch a frozen Core operation in the managed guest broker")
	operation := flag.String("operation", "", "Core operation identity")
	digest := flag.String("digest", "", "frozen Core operation digest")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *runtimeRef != "" {
		if *path != "" || *dataRef != "" || *dataPlan != "" || *tools != "" || flag.NArg() != 0 || personalguest.LaunchRuntime(ctx, *runtimeRef, *operation, *digest, os.Stdout) != nil {
			fmt.Fprintln(os.Stderr, "personal runtime refused or unconfirmed; retain original operation")
			os.Exit(1)
		}
		return
	}
	if *operation != "" || *digest != "" {
		fmt.Fprintln(os.Stderr, "runtime identity requires the fixed launcher")
		os.Exit(1)
	}
	if *dataRef != "" {
		if *path != "" || *dataPlan != "" || *tools != "" || flag.NArg() != 0 || personalguest.ReconcileData(ctx, *dataRef, os.Stdout) != nil {
			fmt.Fprintln(os.Stderr, "managed personal data reconciliation refused")
			os.Exit(1)
		}
		return
	}
	if *dataPlan != "" {
		if *path != "" || *tools != "" || flag.NArg() != 0 || personalguest.InitializeData(ctx, *dataPlan, os.Stdout) != nil {
			fmt.Fprintln(os.Stderr, "personal data initialization refused or unconfirmed; retain the original operation")
			os.Exit(1)
		}
		return
	}
	if err := personalguest.RunWithTools(ctx, *path, *tools); err != nil {
		fmt.Fprintln(os.Stderr, "personal guest stopped: enrollment, authority, transport or durable state unavailable")
		os.Exit(1)
	}
}
