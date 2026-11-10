package delivery

import (
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestValidateRuntimeDependenciesRequiresLateBindings(t *testing.T) {
	// Embedded ports provide non-nil bindings without exercising their methods:
	// this gate validates assembly, while worker behavior has separate fixtures.
	s := &Service{
		applications: struct{ ApplicationReader }{}, catalog: struct{ CatalogReader }{},
		builds: struct{ BuildReader }{}, workflows: struct{ WorkflowReader }{}, releases: struct{ ReleaseReader }{},
		repository: struct{ domaindelivery.Repository }{}, execution: struct{ ExecutionController }{},
		targets: struct{ TargetReader }{}, permissions: &appaccess.PermissionResolver{},
		executionPrincipals: struct{ ExecutionPrincipalReader }{}, manifestPackages: struct{ ManifestPackageWriter }{},
		referenceManifests: struct {
			ServiceReferenceManifestConfigurationReader
		}{},
		manifestDelivery: struct{ ManifestDelivery }{}, docker: struct{ DockerDelivery }{},
		helm: HelmDeliveryDependencies{Charts: struct{ HelmChartLoader }{}, Runtime: struct{ HelmDeliveryRuntime }{}, Secrets: struct{ HelmSecretResolver }{}},
	}
	if err := s.ValidateRuntimeDependencies(); err == nil || !strings.Contains(err.Error(), "template updates") {
		t.Fatalf("incomplete assembly accepted: %v", err)
	}
	var missingUpdates *struct{ TemplateUpdates }
	s.SetTemplateUpdates(missingUpdates)
	if err := s.ValidateRuntimeDependencies(); err == nil || !strings.Contains(err.Error(), "template updates") {
		t.Fatalf("typed-nil late binding accepted: %v", err)
	}
	s.SetTemplateUpdates(struct{ TemplateUpdates }{})
	if err := s.ValidateRuntimeDependencies(); err != nil {
		t.Fatal(err)
	}
	s.SetServiceReferenceManifestConfigurationReader(nil)
	if err := s.ValidateRuntimeDependencies(); err == nil || !strings.Contains(err.Error(), "Manifest reference configuration") {
		t.Fatalf("missing reference projection accepted: %v", err)
	}
}
