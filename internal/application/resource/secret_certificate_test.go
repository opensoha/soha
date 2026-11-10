package resource

import (
	"context"
	"errors"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type certificateDirectStub struct {
	DirectConfiguration
	called bool
}

func (d *certificateDirectStub) GetSecretCertificate(_ context.Context, clusterID, namespace, name string) (domainresource.SecretCertificateView, error) {
	d.called = true
	if clusterID != "cluster-a" || namespace != "team-a" || name != "web-tls" {
		panic("certificate scope changed")
	}
	return domainresource.SecretCertificateView{Name: name, Namespace: namespace, Status: "missing"}, nil
}

func TestSecretCertificateRequiresScopedViewButNotSecretData(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "public metadata only"}[allow], func(t *testing.T) {
			direct := &certificateDirectStub{}
			runtimePermissions := &recordingDenyRuntimePermissions{}
			access := &resourceAccess{
				audit:      discardAuditRecorder{},
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}},
				authorizer: exactPermissionAuthorizer{"platform.configuration.secrets.view": allow}, permissions: runtimePermissions,
			}
			service := &Configuration{resourceAccess: access, direct: direct}
			value, err := service.GetSecretCertificate(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", "team-a", "web-tls")
			if !allow {
				if !errors.Is(err, apperrors.ErrAccessDenied) || direct.called {
					t.Fatalf("denied read reached backend: %v", err)
				}
			} else if err != nil || !direct.called || value.Name != "web-tls" {
				t.Fatalf("metadata read = %+v, %v", value, err)
			}
			if runtimePermissions.key != "" {
				t.Fatalf("public metadata requested sensitive permission %s", runtimePermissions.key)
			}
		})
	}
}

func TestSecretCertificateRejectsMissingScope(t *testing.T) {
	for _, pair := range [][2]string{{"", "web-tls"}, {"team-a", ""}, {" ", "web-tls"}} {
		_, err := (&Configuration{}).GetSecretCertificate(context.Background(), domainidentity.Principal{}, "cluster-a", pair[0], pair[1])
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("missing scope = %v", err)
		}
	}
}
