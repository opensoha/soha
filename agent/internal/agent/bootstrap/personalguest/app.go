package personalguest

import (
	"context"
	"encoding/json"
	"io"

	"github.com/opensoha/soha-agent/internal/agent/config"
	guestclient "github.com/opensoha/soha-agent/internal/agent/environment/personalguest"
)

func InitializeData(ctx context.Context, path string, output io.Writer) error {
	result, err := guestclient.InitializeData(ctx, path)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func ReconcileData(ctx context.Context, dataRef string, output io.Writer) error {
	result, err := guestclient.ReconcileData(ctx, dataRef)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func Run(ctx context.Context, path string) error {
	return RunWithTools(ctx, path, "")
}

func LaunchRuntime(ctx context.Context, dataRef, operation, digest string, output io.Writer) error {
	return guestclient.LaunchRuntime(ctx, dataRef, operation, digest, output)
}

func RunWithTools(ctx context.Context, path, tools string) error {
	cfg, err := config.LoadPersonalGuest(path)
	if err != nil {
		return err
	}
	client, err := guestclient.New(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- client.Run(ctx) }()
	go func() { results <- guestclient.RunRuntime(ctx, cfg, tools) }()
	err = <-results
	cancel()
	other := <-results
	if err != nil {
		return err
	}
	return other
}
