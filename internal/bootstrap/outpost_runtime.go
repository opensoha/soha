package bootstrap

import (
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	appoutpostruntime "github.com/opensoha/soha/internal/application/outpostruntime"
	domainoutpostruntime "github.com/opensoha/soha/internal/domain/outpostruntime"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	outpostruntimerepo "github.com/opensoha/soha/internal/repository/outpostruntime"
)

func newOutpostRuntimeServices(cfg cfgpkg.Config, infra *infrastructure, permissions *appaccess.PermissionResolver, audit *appaudit.Service, execution *appexecution.Service) (*appoutpostruntime.Service, *appoutpostruntime.ExecutorService, error) {
	slots := make([]domainoutpostruntime.Slot, 0, len(cfg.Runtime.OutpostSlots))
	for _, slot := range cfg.Runtime.OutpostSlots {
		slots = append(slots, domainoutpostruntime.Slot{
			ID: slot.ID, OutpostID: slot.OutpostID, ExecutorID: slot.ExecutorID,
			Namespace: slot.Namespace, Name: slot.Name, UID: slot.UID,
			TemplateDigest: slot.TemplateDigest, MaxReplicas: slot.MaxReplicas,
			AllowAdoption: slot.AllowAdoption, PreviousOwnerStopped: slot.PreviousOwnerStopped,
		})
	}
	if len(slots) > 0 {
		_, key, err := cfg.Security.OutpostSigningKey()
		if err != nil || len(key) == 0 {
			return nil, nil, fmt.Errorf("managed Outpost slots require a configured signing key")
		}
	}
	service, err := appoutpostruntime.New(outpostruntimerepo.New(infra.databaseStore.DB()), permissions, slots, audit)
	if err != nil {
		return nil, nil, err
	}
	credentials := make([]appoutpostruntime.ExecutorCredential, 0, len(cfg.Runtime.OutpostRuntimeExecutors))
	for _, item := range cfg.Runtime.OutpostRuntimeExecutors {
		token, err := item.ReadToken()
		if err != nil {
			return nil, nil, fmt.Errorf("load Outpost executor credential: %w", err)
		}
		if token == cfg.Runtime.ExecutionRunnerToken {
			return nil, nil, fmt.Errorf("Outpost executor requires a dedicated credential")
		}
		credentials = append(credentials, appoutpostruntime.ExecutorCredential{ExecutorID: item.ExecutorID, Token: token})
	}
	executor, err := appoutpostruntime.NewExecutorService(service, execution, credentials)
	if err != nil {
		return nil, nil, err
	}
	execution.AddExecutionTaskSink(service)
	return service, executor, nil
}
