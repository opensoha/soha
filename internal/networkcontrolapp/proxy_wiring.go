package networkcontrolapp

import (
	"context"
	control "github.com/opensoha/soha/internal/application/networkcontrol"
	proxy "github.com/opensoha/soha/internal/application/networkproxy"
	deployment "github.com/opensoha/soha/internal/application/proxydeployment"
	publication "github.com/opensoha/soha/internal/application/proxypublication"
	audit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	proxyDomain "github.com/opensoha/soha/internal/domain/networkproxy"
	deploymentDomain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/infrastructure/config"
	publicationInfra "github.com/opensoha/soha/internal/infrastructure/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/proxysource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	proxyRepo "github.com/opensoha/soha/internal/repository/networkproxy"
	runtimeRepo "github.com/opensoha/soha/internal/repository/networkruntime"
	deploymentRepo "github.com/opensoha/soha/internal/repository/proxydeployment"
	publicationRepo "github.com/opensoha/soha/internal/repository/proxypublication"
	"gorm.io/gorm"
)

type proxyControlServices struct {
	runtime     *proxy.Runtime
	publication *publication.Bundles
	deployment  *deployment.Service
}

func newProxyControlServices(db *gorm.DB, cfg config.NetworkControlConfig, service *control.Service, identities *runtimeRepo.Repository) (proxyControlServices, error) {
	var result proxyControlServices
	instances := proxyRepo.New(db)
	runtime, err := proxy.NewRuntime(instances, identities, cfg.CredentialEncryptionKeys)
	if err != nil {
		return result, err
	}
	slots := make([]deploymentDomain.Slot, 0, len(cfg.ProxySlots))
	for _, slot := range cfg.ProxySlots {
		slots = append(slots, slot.Slot())
	}
	deny := proxyRuntimeOnlyConsole{}
	lifecycle, err := deployment.New(deploymentRepo.New(db), nil, slots, deny, deny)
	if err != nil {
		return result, err
	}
	store := publicationRepo.New(db)
	destination, err := publication.NewDestinationPolicy(store, proxysource.Resolver{})
	if err != nil {
		return result, err
	}
	endpoint, err := control.NewProjectionAuthorizer(service, identities)
	if err != nil {
		return result, err
	}
	pool, err := publicationInfra.NewPool(lifecycle, instances, cfg.ProxySlots)
	if err != nil {
		return result, err
	}
	bundles, err := publication.NewBundles(store, store, store, deny, deny, pool, proxyRuntimeDesiredReader{instances}, proxysource.NewCompiler(), service, endpoint, identities, destination, cfg.CredentialEncryptionKeys)
	if err != nil {
		return result, err
	}
	runtime.SetPublicationReader(bundles)
	return proxyControlServices{runtime: runtime, publication: bundles, deployment: lifecycle}, nil
}

// The independent control server exposes enrolled-runtime operations only.
// Accidentally calling a console mutation through these adapters fails closed.
type proxyRuntimeOnlyConsole struct{}

func (proxyRuntimeOnlyConsole) AuthorizeDeployment(context.Context, identity.Principal, string, bool) error {
	return apperrors.ErrAccessDenied
}
func (proxyRuntimeOnlyConsole) Authorize(context.Context, identity.Principal, string, string) error {
	return apperrors.ErrAccessDenied
}
func (proxyRuntimeOnlyConsole) RecordPublication(context.Context, identity.Principal, string, string, string, int64) error {
	return apperrors.ErrAccessDenied
}
func (proxyRuntimeOnlyConsole) Record(context.Context, audit.Entry) error {
	return apperrors.ErrAccessDenied
}

type proxyRuntimeDesiredReader struct{ *proxyRepo.Repository }

func (proxyRuntimeDesiredReader) CommitDesiredBatch(context.Context, identity.Principal, proxyDomain.DesiredBatch) error {
	return apperrors.ErrAccessDenied
}
