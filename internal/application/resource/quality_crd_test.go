package resource

import (
	"context"
	"errors"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type qualityCRDDirect struct {
	observedCRDDefinitionDirect
	read bool
}

func (d *qualityCRDDirect) ListCRDs(context.Context, string) ([]domainresource.CRDView, error) {
	d.read = true
	return nil, nil
}

func TestCRDDeniedClusterScopeNeverReachesDirectAdapter(t *testing.T) {
	for _, action := range []domainaccess.Action{domainaccess.ActionList, domainaccess.ActionDelete} {
		t.Run(string(action), func(t *testing.T) {
			direct := &qualityCRDDirect{}
			authorizer := &recordingLogAuthorizer{allowed: false}
			service := New(Dependencies{DirectCustom: direct, Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "denied-cluster"}}}, Authorizer: authorizer, Audit: noopResourceAuditRecorder{}})
			var err error
			if action == domainaccess.ActionList {
				_, err = service.CustomResources().ListCRDs(t.Context(), domainidentity.Principal{}, "denied-cluster")
			} else {
				err = service.CustomResources().DeleteCRDDefinition(t.Context(), domainidentity.Principal{}, "denied-cluster", "widgets.example.com", "exact-uid")
			}
			if !errors.Is(err, apperrors.ErrAccessDenied) || direct.read || direct.target != "" {
				t.Fatalf("denied scope reached adapter: err=%v read=%v delete=%q", err, direct.read, direct.target)
			}
			if authorizer.request.Cluster.ClusterID != "denied-cluster" || authorizer.request.Action != action {
				t.Fatalf("authorization used wrong scope: %+v", authorizer.request)
			}
		})
	}
}
