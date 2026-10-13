package bootstrap

import (
	"context"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	appvm "github.com/opensoha/soha/internal/application/virtualization"
	delivery "github.com/opensoha/soha/internal/domain/delivery"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

type sshExecutionHydrator struct {
	delivery       appexecution.ExecutionTaskHydrator
	virtualization *appvm.Service
}

func (h sshExecutionHydrator) HydrateExecutionTask(ctx context.Context, task delivery.ExecutionTask) (delivery.ExecutionTask, error) {
	if task.TaskKind == "vm_ssh_command" {
		return h.virtualization.HydrateSSHExecutionTask(ctx, task)
	}
	return h.delivery.HydrateExecutionTask(ctx, task)
}

func newVMSSHOptions(cfg cfgpkg.Config, infra *infrastructure, repos *repositories, core *coreServices) appvm.SSHOptions {
	return appvm.SSHOptions{
		Access: repos.virtualizationRepository, Secrets: core.secretService,
		ControlPlaneEndpoint: func() string {
			if core.runtimeConfigService != nil {
				return core.runtimeConfigService.AccessURL()
			}
			return cfg.HTTP.AccessURL
		},
		AuthorizeExecutionCluster:  core.clusterService.ValidatePersonalExecutionAccess,
		AuthenticateExecutionAgent: core.clusterService.AuthenticateAgentExecution,
		Commands:                   repos.deliveryRepository,
		CancelCommand:              core.executionService.CancelExecutionTask,
		CurrentSessionPrincipal:    core.identityService.CurrentSessionPrincipal,
		WithinCommand: func(ctx context.Context, id string, apply func(context.Context) error) error {
			return dbtx.Within(ctx, infra.databaseStore.DB(), func(txCtx context.Context) error {
				if err := dbtx.DB(txCtx, infra.databaseStore.DB()).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "vm-ssh-command/"+id).Error; err != nil {
					return err
				}
				return apply(txCtx)
			})
		},
		Executor: agentinfra.VMSSHExecutor{Client: func(ctx context.Context, id string) (*agentinfra.Client, error) {
			connection, err := repos.clusterRepository.GetConnection(ctx, id)
			if err != nil {
				return nil, err
			}
			return infra.agentRegistry.ClientFor(connection)
		}},
	}
}
