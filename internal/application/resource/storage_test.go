package resource

import (
	"context"
	"errors"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type storageRelationAuthorizer struct{}

func (storageRelationAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	if request.Resource.Kind == "Pod" {
		return domainaccess.Decision{Allowed: false, Reason: "workloads denied"}, nil
	}
	if request.Namespace.Namespace == "broken" {
		return domainaccess.Decision{}, errors.New("authorization unavailable")
	}
	return domainaccess.Decision{Allowed: true}, nil
}

func TestStorageRelationsOmitUnauthorizedEnrichment(t *testing.T) {
	access := &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: storageRelationAuthorizer{},
	}
	principal := domainidentity.Principal{UserID: "viewer"}

	pods := filterStoragePods(context.Background(), access, principal, "cluster-a", []domainresource.StoragePodReferenceView{{Name: "private", Namespace: "team-a"}})
	if len(pods) != 0 {
		t.Fatalf("pods = %#v, want unauthorized enrichment omitted", pods)
	}
	claims := filterStorageClaims(context.Background(), access, principal, "cluster-a", []domainresource.PersistentVolumeClaimView{
		{Name: "visible", Namespace: "team-a"},
		{Name: "error", Namespace: "broken"},
	})
	if len(claims) != 1 || claims[0].Name != "visible" {
		t.Fatalf("claims = %#v, want only visible relation", claims)
	}
}
