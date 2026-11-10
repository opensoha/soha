package delivery

import (
	"strings"
	"testing"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestServiceReferenceCandidateIdentitySeparatesConfigurationAndOwnership(t *testing.T) {
	makePreview := func() ServiceReferencePreview {
		return ServiceReferencePreview{ApplicationID: "app", ServiceID: "svc", Source: ServiceReferenceScope{ApplicationEnvironmentID: "source"},
			Targets: []ServiceReferenceTarget{{TargetID: "target", ClusterID: "cluster", Namespace: "backend", WorkloadKind: "Deployment", WorkloadName: "api", OwnershipMode: "observe_only",
				Resources: []ServiceReferenceResource{{Resource: domainresource.ResourceRef{ClusterID: "cluster", Namespace: "backend", APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "uid", ScopeMode: domainresource.ResourceScopeModeNamespace}, SpecDigest: strings.Repeat("a", 64)}}}}}
	}
	before, err := ServiceReferenceIdentityRevision(makePreview())
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name   string
		change func(*ServiceReferencePreview)
		same   bool
	}{
		{name: "configuration", same: true, change: func(p *ServiceReferencePreview) { p.Targets[0].Resources[0].SpecDigest = strings.Repeat("b", 64) }},
		{name: "observations", same: true, change: func(p *ServiceReferencePreview) {
			p.ObservedAt = time.Now()
			p.Source.UpdatedAt = time.Now()
			p.SourceRevision = strings.Repeat("c", 64)
			p.Targets[0].HelmRevision = 8
			p.Targets[0].Resources[0].Resource.ResourceVersion = "new"
		}},
		{name: "uid", change: func(p *ServiceReferencePreview) { p.Targets[0].Resources[0].Resource.UID = "replacement" }},
		{name: "control", change: func(p *ServiceReferencePreview) { p.Targets[0].OwnershipMode = "managed" }},
		{name: "target", change: func(p *ServiceReferencePreview) { p.Targets[0].TargetID = "replacement" }},
		{name: "location", change: func(p *ServiceReferencePreview) { p.Targets[0].Namespace = "other" }},
		{name: "crd", change: func(p *ServiceReferencePreview) { p.Targets[0].Resources[0].Plural = "other" }},
		{name: "source binding", change: func(p *ServiceReferencePreview) { p.Source.ApplicationEnvironmentID = "other" }},
		{name: "service", change: func(p *ServiceReferencePreview) { p.ServiceID = "other" }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			p := makePreview()
			scenario.change(&p)
			after, err := ServiceReferenceIdentityRevision(p)
			if err != nil || (before == after) != scenario.same {
				t.Fatalf("same=%v err=%v", before == after, err)
			}
		})
	}
}
