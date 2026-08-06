package resource

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type recordingDenyRuntimePermissions struct {
	key string
}

func (p *recordingDenyRuntimePermissions) Authorize(_ context.Context, _ domainidentity.Principal, key string) error {
	p.key = key
	return apperrors.ErrAccessDenied
}

func TestSensitiveReadsRequireDedicatedRuntimePermission(t *testing.T) {
	tests := []struct {
		name    string
		wantKey string
		call    func(context.Context, domainidentity.Principal, *resourceAccess) error
	}{
		{
			name:    "secret detail",
			wantKey: appaccess.PermPlatformConfigurationSecretDataView,
			call: func(ctx context.Context, principal domainidentity.Principal, access *resourceAccess) error {
				_, err := (&Configuration{resourceAccess: access}).GetSecretDetail(ctx, principal, "cluster-a", "team-a", "registry")
				return err
			},
		},
		{
			name:    "secret yaml",
			wantKey: appaccess.PermPlatformConfigurationSecretDataView,
			call: func(ctx context.Context, principal domainidentity.Principal, access *resourceAccess) error {
				_, err := (&GenericResources{resourceAccess: access}).GetResourceYAML(ctx, principal, "cluster-a", "team-a", " secret ", "registry")
				return err
			},
		},
		{
			name:    "helm values",
			wantKey: appaccess.PermPlatformHelmValuesView,
			call: func(ctx context.Context, principal domainidentity.Principal, access *resourceAccess) error {
				_, err := (&Helm{resourceAccess: access}).GetHelmReleaseValues(ctx, principal, "cluster-a", "team-a", "soha", "")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			permissions := &recordingDenyRuntimePermissions{}
			access := &resourceAccess{
				resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
					ID:             "cluster-a",
					ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
				}}},
				authorizer:  allowAllResourceAuthorizer{},
				permissions: permissions,
			}

			err := test.call(context.Background(), domainidentity.Principal{UserID: "readonly"}, access)
			if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("error = %v, want access denied", err)
			}
			if permissions.key != test.wantKey {
				t.Fatalf("permission = %q, want %q", permissions.key, test.wantKey)
			}
		})
	}
}
