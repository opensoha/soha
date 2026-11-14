package runner

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

type runnerProviderProbe struct{ runner *Runner }

func (p runnerProviderProbe) Check(ctx context.Context, provider AgentProviderDefinition) error {
	switch provider.Runtime.Kind {
	case "cli":
		command := provider.commandSpec().Command
		if configured := p.runner.cfg.AgentRuntime.Providers[provider.ID].Command; configured != "" {
			command = configured
		}
		_, err := exec.LookPath(command)
		if err != nil {
			return errors.New("agent command is unavailable")
		}
		return nil
	case "remote":
		client, err := p.runner.hermesClientFor(provider)
		if err != nil {
			return err
		}
		return client.checkReady(ctx)
	default:
		return errors.New("agent runtime has no executable adapter")
	}
}

func (r *Runner) refreshProviderHealth(ctx context.Context) {
	if r.providerRegistry == nil || r.providerConformance == nil {
		return
	}
	for _, status := range r.providerRegistry.Statuses() {
		provider, ok := r.providerRegistry.Resolve(status.ProviderID, status.ProviderVersion)
		if !ok {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := r.providerConformance.Check(probeCtx, provider)
		cancel()
		health, reason := "healthy", ""
		if err != nil {
			health, reason = "unhealthy", "agent runtime readiness check failed"
		}
		_ = r.providerRegistry.SetHealth(provider.ID, health, reason, time.Now().UTC())
	}
}
