package manifestruntime

import (
	"context"
	"fmt"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
)

type Agent struct{ client *agentinfra.Client }

func NewAgent(registry *agentinfra.Registry, connection domaincluster.Connection) (*Agent, error) {
	client, err := registry.ClientFor(connection)
	return &Agent{client: client}, err
}

func (a *Agent) Execute(ctx context.Context, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	if payload.Action != domainmanifest.TaskActionObserve && payload.Action != domainmanifest.TaskActionRolloutControl {
		return domainmanifest.TaskResult{}, fmt.Errorf("native rollout RPC only supports observation and control of queued executions")
	}
	return a.client.ExecuteManifestRollout(ctx, payload)
}
