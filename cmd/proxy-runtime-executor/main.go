// proxy-runtime-executor is a separate, installer-authorized workload. The
// control-plane server never starts it or inherits its Kubernetes credentials.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	runtime "github.com/opensoha/soha/internal/infrastructure/proxydeployment"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type config struct {
	ControlPlaneURL string        `json:"controlPlaneUrl"`
	ExecutorID      string        `json:"executorId"`
	TokenFile       string        `json:"tokenFile"`
	Slots           []domain.Slot `json:"slots"`
}

func run(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Proxy executor installation config")
	}
	defer func() { _ = file.Close() }()
	var cfg config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return fmt.Errorf("invalid Proxy executor installation config")
	}
	token, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return fmt.Errorf("read dedicated Proxy executor credential")
	}
	client, err := runtime.NewHTTPClient(cfg.ControlPlaneURL, cfg.ExecutorID, strings.TrimSpace(string(token)))
	if err != nil {
		return err
	}
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("Proxy executor requires its installer-authorized in-cluster service account")
	}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("construct Proxy executor Kubernetes client")
	}
	scaler, err := runtime.NewScaleClient(kubeClient)
	if err != nil {
		return err
	}
	worker, err := runtime.NewWorker(client, scaler, cfg.ExecutorID, cfg.Slots)
	if err != nil {
		return err
	}
	return worker.Run(ctx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	path := os.Getenv("SOHA_PROXY_EXECUTOR_CONFIG")
	if path == "" {
		path = "/etc/soha-proxy-executor/config.json"
	}
	if err := run(ctx, path); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("Proxy executor stopped", "reason", err.Error())
		os.Exit(1)
	}
}
