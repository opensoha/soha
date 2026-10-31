package bootstrap

import (
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	apptransfer "github.com/opensoha/soha/internal/application/contenttransfer"
	appendpoint "github.com/opensoha/soha/internal/application/endpointcontrol"
	appsecret "github.com/opensoha/soha/internal/application/secret"
	appintegration "github.com/opensoha/soha/internal/application/systemintegration"
	infratransfer "github.com/opensoha/soha/internal/infrastructure/contenttransfer"
	storage "github.com/opensoha/soha/internal/infrastructure/softwareobjectstore"
	transferrepo "github.com/opensoha/soha/internal/repository/contenttransfer"
	endpointrepo "github.com/opensoha/soha/internal/repository/endpointcontrol"
)

type contentSecurityServices struct {
	content  *appdlp.Service
	managed  *apptransfer.Service
	endpoint *appendpoint.Service
}

// Assemble the three content-security owners together; each keeps its own
// policy, state store and enforcement boundary.
func newContentSecurityServices(infra *infrastructure, repos *repositories, scope *appaccess.Service, permissions *appaccess.PermissionResolver, connections *appintegration.Service, secrets *appsecret.Service, stepUp appdlp.StepUpVerifier) (contentSecurityServices, error) {
	var result contentSecurityServices
	var err error
	result.content, err = newContentDLPService(infra.databaseStore.DB(), repos.dockerRepository, scope, permissions, contentNetworkLinkage{db: infra.databaseStore.DB(), runtime: repos.networkRuntimeRepository}, stepUp)
	if err != nil {
		return result, err
	}
	store, err := transferrepo.New(infra.databaseStore.DB())
	if err != nil {
		return result, err
	}
	result.managed, err = apptransfer.New(store, &infratransfer.Factory{Connections: connections, Storage: storage.NewProvider(connections), Secrets: secrets}, scope, permissions, apptransfer.WithNetworkLinkage(managedContentNetworkLinkage{contentNetworkLinkage{db: infra.databaseStore.DB(), runtime: repos.networkRuntimeRepository}}), apptransfer.WithStepUp(stepUp))
	if err != nil {
		return result, err
	}
	result.endpoint, err = appendpoint.New(endpointrepo.New(infra.databaseStore.DB()), repos.networkAccessRepository, scope, permissions)
	return result, err
}
