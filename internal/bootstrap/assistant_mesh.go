package bootstrap

import (
	"context"

	apihandlers "github.com/opensoha/soha/internal/api/handlers"
	apiroutes "github.com/opensoha/soha/internal/api/routes"
	appmesh "github.com/opensoha/soha/internal/application/assistantmesh"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	meshrepo "github.com/opensoha/soha/internal/repository/assistantmesh"
)

type assistantMeshRunner struct{ copilot *appcopilot.Service }

func (r assistantMeshRunner) LaunchMeshSkill(ctx context.Context, principal identity.Principal, spec appmesh.RunSpec) (string, error) {
	return r.copilot.LaunchAssistantMeshSkill(ctx, principal, spec.SessionID, spec.RequestID, spec.Prompt,
		domaincopilot.AssistantMeshSkill{AssetID: spec.AssetID, Digest: spec.Digest, Version: spec.Version, Trial: spec.Trial, Parameters: spec.Parameters})
}

func (r assistantMeshRunner) MeshRunStatus(ctx context.Context, principal identity.Principal, runID, assetID, digest string) (string, error) {
	return r.copilot.AssistantMeshRunStatus(ctx, principal, runID, assetID, digest)
}

func wireAssistantMeshRoutes(deps *apiroutes.Dependencies, infra *infrastructure, repos *repositories, core *coreServices, delivery *deliveryServices, gateway *gatewayServices) {
	runner := assistantMeshRunner{copilot: delivery.copilotService}
	store := meshrepo.New(infra.databaseStore.DB())
	service := appmesh.New(store, core.settingsService, runner, repos.identityRepository, core.permissionResolver, core.auditService)
	sharedMCP := appmesh.NewMCPService(store, core.settingsService, core.permissionResolver, gateway.aiGatewayService, core.auditService, repos.identityRepository)
	delivery.copilotService.SetAssistantMeshSkills(service)
	delivery.copilotService.SetAssistantMeshMCP(sharedMCP)
	deps.AssistantMesh = apihandlers.NewAssistantMeshHandler(service, delivery.copilotService, sharedMCP)
}
