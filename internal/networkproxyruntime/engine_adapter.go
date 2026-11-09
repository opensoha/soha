package networkproxyruntime

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

// engineAdapter contains kernel syntax and local control protocol differences only.
// The host owns process start/stop, configuration files, authorization, protection,
// bootstrap, egress and acknowledgements. A future trusted stdio implementation
// must cross this boundary without receiving the control client or its identity.
// Check and run share commandArgs: builtins use the same owned-process lifecycle.
type engineAdapter interface {
	render(string) ([]byte, error)
	commandArgs(path string, check bool) []string
	health(context.Context) error
	observe(context.Context) (domain.Observation, []domain.Connection)
	supportsClose() bool
	close(context.Context, string) (string, error)
}

func newBuiltinEngineAdapter(config Config, secret string, controller *clashController) engineAdapter {
	if config.Engine == domain.EngineV2Ray {
		return &v2rayEngineAdapter{config: config}
	}
	return &clashEngineAdapter{config: config, secret: secret, controller: controller}
}

type clashEngineAdapter struct {
	config     Config
	secret     string
	controller *clashController
}

func (a *clashEngineAdapter) render(content string) ([]byte, error) {
	controller, err := url.Parse(a.config.ControllerURL)
	if err != nil {
		return nil, err
	}
	switch a.config.Engine {
	case domain.EngineMihomo:
		return renderMihomo(content, controller.Host, a.secret)
	case domain.EngineSingBox:
		return renderSingBox(content, controller.Host, a.secret)
	default:
		return nil, fmt.Errorf("proxy engine is unsupported")
	}
}

func (a *clashEngineAdapter) commandArgs(path string, check bool) []string {
	switch a.config.Engine {
	case domain.EngineMihomo:
		if check {
			return []string{"-d", filepath.Dir(path), "-t", "-f", path}
		}
		return []string{"-d", filepath.Dir(path), "-f", path}
	case domain.EngineSingBox:
		if check {
			return []string{"check", "-c", path}
		}
		return []string{"run", "-c", path}
	default:
		return nil
	}
}

func (a *clashEngineAdapter) health(ctx context.Context) error {
	_, err := a.controller.version(ctx)
	return err
}

func (a *clashEngineAdapter) observe(ctx context.Context) (domain.Observation, []domain.Connection) {
	sample := domain.Observation{EngineVersion: "unknown", Health: "degraded", Capabilities: []string{}}
	version, err := a.controller.version(ctx)
	if err != nil {
		sample.ReasonCode = "controller_unavailable"
		return sample, nil
	}
	sample.EngineVersion = version
	upload, download, active, connections, err := a.controller.snapshot(ctx)
	if err != nil {
		sample.ReasonCode = "traffic_unavailable"
		return sample, nil
	}
	sample.Health = "healthy"
	sample.Capabilities = []string{"traffic", "connections", "close_connection", "protected_destinations_v1", "guarded_dns_tcp_v1"}
	if a.config.Engine == domain.EngineMihomo {
		sample.Capabilities = append(sample.Capabilities, "protection_rules_readback")
	}
	sample.UploadTotal, sample.DownloadTotal = upload, download
	sample.ActiveConnections = &active
	return sample, connections
}

func (a *clashEngineAdapter) supportsClose() bool { return true }
func (a *clashEngineAdapter) close(ctx context.Context, id string) (string, error) {
	return a.controller.close(ctx, id)
}

type v2rayEngineAdapter struct{ config Config }

func (a *v2rayEngineAdapter) render(content string) ([]byte, error) {
	controller, err := url.Parse(a.config.ControllerURL)
	if err != nil {
		return nil, err
	}
	return renderV2Ray(content, controller.Host)
}

func (a *v2rayEngineAdapter) commandArgs(path string, check bool) []string {
	operation := "run"
	if check {
		operation = "test"
	}
	return []string{operation, "-format=json", "-config", path}
}

func (a *v2rayEngineAdapter) observe(ctx context.Context) (domain.Observation, []domain.Connection) {
	sample := domain.Observation{EngineVersion: "unknown", Health: "degraded", Capabilities: []string{}}
	version, upload, download, err := a.stats(ctx)
	if err != nil {
		sample.ReasonCode = "stats_unavailable"
		return sample, nil
	}
	sample.EngineVersion, sample.Health = version, "healthy"
	sample.Capabilities = []string{"traffic", "protected_destinations_v1", "guarded_dns_tcp_v1"}
	sample.UploadTotal, sample.DownloadTotal = upload, download
	return sample, nil
}

func (a *v2rayEngineAdapter) supportsClose() bool { return false }
func (a *v2rayEngineAdapter) close(context.Context, string) (string, error) {
	return "failed", fmt.Errorf("connection close is unsupported")
}
