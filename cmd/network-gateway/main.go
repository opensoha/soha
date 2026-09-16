package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/opensoha/soha/internal/networkgateway"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/redaction"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	mode := "run"
	if len(args) == 1 {
		mode = args[0]
	}
	if len(args) > 1 || (mode != "run" && mode != "enroll") {
		_, _ = fmt.Fprintln(os.Stderr, "usage: network-gateway [run|enroll]")
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := networkgateway.LoadConfig()
	if err != nil {
		return fail(err)
	}
	privateKey, err := networkgateway.LoadOrCreatePrivateKey(config.WireGuardPrivateKeyFile)
	if err != nil {
		return fail(err)
	}
	httpClient, devicePublicKey, err := config.ControlHTTPClient()
	if err != nil {
		return fail(err)
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		return fail(err)
	}
	control, err := networkgateway.NewControlClient(config.ControlURL, config.RuntimeID, httpClient, schemas, config.MaxClockSkew)
	if err != nil {
		return fail(err)
	}
	if mode == "enroll" {
		enrollment, err := config.Enrollment(privateKey.PublicKey().String(), devicePublicKey, clientVersion())
		if err != nil {
			return fail(err)
		}
		result, err := control.Enroll(context.Background(), enrollment)
		if err != nil {
			return fail(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"accepted": result.Accepted, "reasonCode": result.ReasonCode})
		return 0
	}
	return runGateway(config, privateKey, control, schemas, logger)
}

func runGateway(config networkgateway.Config, privateKey [32]byte, control *networkgateway.ControlClient, schemas *networkprotocol.Schemas, logger *slog.Logger) int {
	system, err := networkgateway.NewLinuxSystem(config.IPPath, config.NFTPath, config.EgressInterface)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = system.Close() }()
	executor, err := networkgateway.NewExecutor(privateKey, system, config.EgressInterface)
	if err != nil {
		return fail(err)
	}
	var telemetry networkgateway.Telemetry
	if config.IngestURL != "" {
		client, err := config.IngestHTTPClient()
		if err != nil {
			return fail(err)
		}
		metrics, err := networkgateway.NewTelemetryClient(config.IngestURL, config.RuntimeID, client, schemas)
		if err != nil {
			return fail(err)
		}
		metrics.SetVPNPeerCounters(system.VPNPeerCounters)
		telemetry = metrics
	}
	runtime, err := networkgateway.NewRuntime(config.RuntimeID, control, executor, telemetry, config.PollInterval, config.HeartbeatInterval, config.MaxClockSkew, logger)
	if err != nil {
		return fail(err)
	}
	health := &http.Server{Addr: config.HealthAddress, Handler: runtime.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(signalCtx)
	defer stop()
	defer cancel()
	runtimeErr := make(chan error, 1)
	healthErr := make(chan error, 2)
	probe, err := config.ProbeServer(runtime)
	if err != nil {
		return fail(err)
	}
	if probe != nil {
		go func() {
			err := probe.ListenAndServeTLS("", "")
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			healthErr <- err
		}()
	}
	go func() { runtimeErr <- runtime.Run(ctx) }()
	go func() {
		err := health.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		healthErr <- err
	}()
	logger.Info("network gateway started", "event", "network_gateway.started", "healthAddress", config.HealthAddress)
	var failure error
	select {
	case <-signalCtx.Done():
	case err := <-runtimeErr:
		failure = err
	case err := <-healthErr:
		failure = err
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	shutdownErr := errors.Join(executor.Disable(shutdownCtx), health.Shutdown(shutdownCtx))
	if probe != nil {
		shutdownErr = errors.Join(shutdownErr, probe.Shutdown(shutdownCtx))
	}
	if failure != nil {
		logger.Error("network gateway exited with error", "event", "network_gateway.run.failed", "error", redaction.LogText(failure.Error(), 2048))
	}
	if shutdownErr != nil {
		logger.Error("network gateway shutdown failed", "event", "network_gateway.shutdown.failed", "error", redaction.LogText(shutdownErr.Error(), 2048))
	}
	if failure != nil || shutdownErr != nil {
		return 1
	}
	return 0
}

func clientVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func fail(err error) int {
	_, _ = fmt.Fprintln(os.Stderr, redaction.LogText(err.Error(), 2048))
	return 1
}
