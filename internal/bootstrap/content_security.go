package bootstrap

import (
	"context"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	apptransfer "github.com/opensoha/soha/internal/application/contenttransfer"
	appendpoint "github.com/opensoha/soha/internal/application/endpointcontrol"
	appsecret "github.com/opensoha/soha/internal/application/secret"
	appintegration "github.com/opensoha/soha/internal/application/systemintegration"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	infratransfer "github.com/opensoha/soha/internal/infrastructure/contenttransfer"
	storage "github.com/opensoha/soha/internal/infrastructure/softwareobjectstore"
	dlprepo "github.com/opensoha/soha/internal/repository/contentdlp"
	transferrepo "github.com/opensoha/soha/internal/repository/contenttransfer"
	endpointrepo "github.com/opensoha/soha/internal/repository/endpointcontrol"
)

type contentSecurityServices struct {
	content    *appdlp.Service
	managed    *apptransfer.Service
	endpoint   *appendpoint.Service
	appHosting *appapphosting.Service
}

// Assemble the three content-security owners together; each keeps its own
// policy, state store and enforcement boundary.
func newContentSecurityServices(ctx context.Context, coreDB cfgpkg.DatabaseConfig, infra *infrastructure, repos *repositories, scope *appaccess.Service, permissions *appaccess.PermissionResolver, connections *appintegration.Service, secrets *appsecret.Service, audit *appaudit.Service, stepUp appdlp.StepUpVerifier, visitorIDSecret string) (contentSecurityServices, error) {
	var result contentSecurityServices
	var err error
	dlpStore, err := dlprepo.New(infra.databaseStore.DB())
	if err != nil {
		return result, err
	}
	result.content, err = newContentDLPService(infra.databaseStore.DB(), repos.dockerRepository, scope, permissions, contentNetworkLinkage{approvals: dlpStore, runtime: repos.networkRuntimeRepository}, stepUp)
	if err != nil {
		return result, err
	}
	store, err := transferrepo.New(infra.databaseStore.DB())
	if err != nil {
		return result, err
	}
	result.managed, err = apptransfer.New(store, &infratransfer.Factory{Connections: connections, Storage: storage.NewProvider(connections), Secrets: secrets}, scope, permissions, apptransfer.WithNetworkLinkage(managedContentNetworkLinkage{contentNetworkLinkage{approvals: store, runtime: repos.networkRuntimeRepository}}), apptransfer.WithStepUp(stepUp))
	if err != nil {
		return result, err
	}
	result.endpoint, err = appendpoint.New(endpointrepo.New(infra.databaseStore.DB()), repos.networkAccessRepository, scope, permissions)
	if err != nil {
		return result, err
	}
	result.appHosting, err = newAppHostingService(ctx, coreDB, infra, repos.identityRepository, visitorIDSecret, permissions, connections, audit)
	return result, err
}
