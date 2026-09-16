package agentharness

import (
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"testing"
	"time"
)

type workbenchObserver struct{ catalog ProviderCatalog }

func (o *workbenchObserver) ApplyProviderCatalog(catalog ProviderCatalog) { o.catalog = catalog }

func TestWorkbenchReadinessRequiresCurrentHealthyAcknowledgement(t *testing.T) {
	source := &mutableExtensionSource{}
	source.replace([]domainplugin.ExtensionRecord{providerExtension("1.0.0")})
	observer := &workbenchObserver{}
	service, err := NewProviderControlPlane(NewProviderReconciler(source), providerPermissionStub{want: appaccess.PermAIAgentProvidersView}, WithProviderCatalogObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	provider := observer.catalog.Providers[0]
	if observer.catalog.RuntimeStatuses[provider.ID].Health == "healthy" {
		t.Fatal("installed plugin was marked healthy without a runner")
	}
	now := time.Now().UTC()
	_, err = service.Acknowledge(RegistryAcknowledgement{RunnerID: "runner", Revision: observer.catalog.Revision, ActiveRevision: observer.catalog.Revision, Accepted: true, Targeted: true, ObservedAt: now, ProviderStatuses: []RunnerProviderStatus{{ProviderID: provider.ID, ProviderVersion: provider.ProviderVersion, CatalogRevision: observer.catalog.Revision, Health: "healthy", ObservedAt: now}}})
	if err != nil {
		t.Fatal(err)
	}
	if observer.catalog.RuntimeStatuses[provider.ID].Health != "healthy" {
		t.Fatal("healthy runner was not projected")
	}
	service.now = func() time.Time { return now.Add(3 * time.Minute) }
	service.mu.Lock()
	service.publishWorkbenchCatalogLocked()
	service.mu.Unlock()
	if observer.catalog.RuntimeStatuses[provider.ID].Health == "healthy" {
		t.Fatal("stale runner remained ready")
	}
	source.replace(nil)
	if _, err := service.RegistrySnapshot("runner"); err != nil {
		t.Fatal(err)
	}
	if len(observer.catalog.Providers) != 0 {
		t.Fatal("disabled plugin remained in workbench")
	}
}
