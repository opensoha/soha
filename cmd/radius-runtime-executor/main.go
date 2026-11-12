package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/infrastructure/config"
	runtime "github.com/opensoha/soha/internal/infrastructure/radiusdeployment"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type executorConfig struct {
	BaseURL    string        `json:"baseUrl"`
	ExecutorID string        `json:"executorId"`
	TokenFile  string        `json:"tokenFile"`
	Kubeconfig string        `json:"kubeconfig"`
	Slots      []domain.Slot `json:"slots"`
}

func main() {
	path := flag.String("config", "", "installer-approved executor JSON")
	flag.Parse()
	if err := run(*path); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "RADIUS executor stopped:", err)
		os.Exit(1)
	}
}
func run(path string) error {
	if path == "" {
		return fmt.Errorf("config file is required")
	}
	file, err := os.Open(path) // #nosec G304 -- The local operator supplies this installation file via --config; it is not a remote task path.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var cfg executorConfig
	if err := decoder.Decode(&cfg); err != nil {
		return fmt.Errorf("invalid executor configuration")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid trailing configuration")
	}
	token, err := (config.RadiusRuntimeExecutorConfig{ExecutorID: cfg.ExecutorID, TokenFile: cfg.TokenFile}).ReadToken()
	if err != nil {
		return err
	}
	client, err := runtime.NewHTTPClient(cfg.BaseURL, cfg.ExecutorID, token)
	if err != nil {
		return err
	}
	var cluster *rest.Config
	if cfg.Kubeconfig == "" {
		cluster, err = rest.InClusterConfig()
	} else {
		cluster, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	}
	if err != nil {
		return fmt.Errorf("kubernetes identity unavailable")
	}
	cluster.Timeout = 5e9
	kube, err := kubernetes.NewForConfig(cluster)
	if err != nil {
		return err
	}
	scale, err := runtime.NewScaleAdapter(kube)
	if err != nil {
		return err
	}
	worker, err := runtime.NewWorker(client, scale, cfg.ExecutorID, cfg.Slots)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return worker.Run(ctx)
}
