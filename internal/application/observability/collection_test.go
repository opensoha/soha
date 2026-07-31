package observability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type memoryCollectionSettings struct {
	values map[string]map[string]any
}

func (m *memoryCollectionSettings) Get(_ context.Context, key string) (map[string]any, bool, error) {
	value, ok := m.values[key]
	return value, ok, nil
}

func (m *memoryCollectionSettings) Upsert(_ context.Context, key, _ string, value map[string]any, _ string) error {
	m.values[key] = value
	return nil
}

type staticCollectionConnection struct{ connection domaincluster.Connection }

func (s staticCollectionConnection) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return s.connection, nil
}

type collectionAuthorizer func(domainaccess.Request) domainaccess.Decision

func (a collectionAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return a(request), nil
}

type recordingCollectionHelm struct {
	installs int
	updates  int
	deletes  int
}

func (h *recordingCollectionHelm) InstallHelmChart(context.Context, domainidentity.Principal, string, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	h.installs++
	return domainresource.HelmChartInstallResult{}, nil
}

func (h *recordingCollectionHelm) UpdateHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error) {
	h.updates++
	return domainresource.HelmValuesView{}, nil
}

func (h *recordingCollectionHelm) DeleteHelmRelease(context.Context, domainidentity.Principal, string, string, string) error {
	h.deletes++
	return nil
}

func newCollectionTestService(t *testing.T, now time.Time, authorizer domainaccess.Authorizer, helm CollectionHelm) (*Service, domainidentity.Principal) {
	t.Helper()
	key, err := keyring.NewKey("collection.test", "01234567890123456789012345678901", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Dependencies{
		DataSources: &memoryDataSources{items: map[string]domainobservability.DataSource{}},
		Permissions: appaccess.NewPermissionResolver(observabilityRoleReader{"admin": {
			appaccess.PermPlatformClustersView,
			appaccess.PermObserveLogCollectionManage,
			appaccess.PermObserveLogDataSourcesManage,
		}}),
		Logs: &recordingLogRegistry{}, Keys: keys,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	service.ConfigureCollection(CollectionDependencies{
		Settings: &memoryCollectionSettings{values: map[string]map[string]any{}},
		Connections: staticCollectionConnection{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "cluster-a", Region: "cn-east", Environment: "prod", Labels: map[string]string{"team": "platform"},
		}}},
		Helm: helm, Access: authorizer,
	})
	return service, domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}}
}

func starterCollectionInput(namespaces ...string) sohaapi.LogCollectionPreflightInput {
	return sohaapi.LogCollectionPreflightInput{
		Profile: sohaapi.LogCollectionProfileStarter, Namespace: "soha-observability",
		NamespaceAllowlist: namespaces, RetentionDays: 7, StorageSize: "10Gi",
	}
}

func TestCollectionPreflightEnforcesPodLogScopeAndDeclaresNoKubernetesRBAC(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	authorizer := collectionAuthorizer(func(request domainaccess.Request) domainaccess.Decision {
		allowed := request.Resource.Kind == "HelmRelease" || request.Namespace.Namespace == "team-a"
		return domainaccess.Decision{Allowed: allowed, Reason: "test policy"}
	})
	service, principal := newCollectionTestService(t, now, authorizer, &recordingCollectionHelm{})

	allowed, err := service.PreflightLogCollection(context.Background(), principal, "cluster-a", starterCollectionInput("team-a"))
	if err != nil || !allowed.CanEnable || allowed.PlanToken == "" || len(allowed.RBACRules) != 0 {
		t.Fatalf("authorized preflight = %#v, error = %v", allowed, err)
	}
	for name, input := range map[string]sohaapi.LogCollectionPreflightInput{
		"cluster-wide":    starterCollectionInput(),
		"other namespace": starterCollectionInput("team-b"),
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := service.PreflightLogCollection(context.Background(), principal, "cluster-a", input)
			if err != nil || plan.CanEnable || plan.PlanToken != "" || len(plan.Blockers) == 0 {
				t.Fatalf("restricted preflight = %#v, error = %v", plan, err)
			}
		})
	}
}

func TestCollectionPlanTokenRejectsTamperingWrongPrincipalAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	service, _ := newCollectionTestService(t, now, collectionAuthorizer(func(domainaccess.Request) domainaccess.Decision {
		return domainaccess.Decision{Allowed: true}
	}), &recordingCollectionHelm{})
	plan := collectionPlan{ClusterID: "cluster-a", UserID: "user-1", ExpiresAt: now.Add(time.Minute)}
	token, err := service.signCollectionPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.verifyCollectionPlan(token, "cluster-a", "user-1"); err != nil {
		t.Fatalf("valid token error = %v", err)
	}
	tampered := token[:len(token)-1] + map[bool]string{true: "B", false: "A"}[strings.HasSuffix(token, "A")]
	if _, err := service.verifyCollectionPlan(tampered, "cluster-a", "user-1"); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("tampered token error = %v", err)
	}
	if _, err := service.verifyCollectionPlan(token, "cluster-a", "user-2"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("wrong principal error = %v", err)
	}
	service.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := service.verifyCollectionPlan(token, "cluster-a", "user-1"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestCollectionLifecycleResumesReleaseAndPreservesHistory(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	helm := &recordingCollectionHelm{}
	service, principal := newCollectionTestService(t, now, collectionAuthorizer(func(domainaccess.Request) domainaccess.Decision {
		return domainaccess.Decision{Allowed: true}
	}), helm)

	preflight, err := service.PreflightLogCollection(context.Background(), principal, "cluster-a", starterCollectionInput("team-a"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := service.EnableLogCollection(context.Background(), principal, "cluster-a", sohaapi.LogCollectionEnableInput{PlanToken: preflight.PlanToken})
	if err != nil || state.Status != sohaapi.LogCollectionStatusHealthy || helm.installs != 1 {
		t.Fatalf("enable state=%#v installs=%d error=%v", state, helm.installs, err)
	}
	state, err = service.DisableLogCollection(context.Background(), principal, "cluster-a", sohaapi.LogCollectionDisableInput{Action: sohaapi.LogCollectionDisableActionStop})
	if err != nil || state.Status != sohaapi.LogCollectionStatusDisabled || !state.HistoryPreserved || helm.updates != 1 {
		t.Fatalf("stop state=%#v updates=%d error=%v", state, helm.updates, err)
	}

	preflight, err = service.PreflightLogCollection(context.Background(), principal, "cluster-a", starterCollectionInput("team-a"))
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.EnableLogCollection(context.Background(), principal, "cluster-a", sohaapi.LogCollectionEnableInput{PlanToken: preflight.PlanToken})
	if err != nil || state.Status != sohaapi.LogCollectionStatusHealthy || helm.installs != 1 || helm.updates != 2 {
		t.Fatalf("resume state=%#v installs=%d updates=%d error=%v", state, helm.installs, helm.updates, err)
	}
	state, err = service.DisableLogCollection(context.Background(), principal, "cluster-a", sohaapi.LogCollectionDisableInput{Action: sohaapi.LogCollectionDisableActionUninstall})
	if err != nil || state.Mode != sohaapi.LogCollectionModeRuntimeOnly || !state.HistoryPreserved || helm.deletes != 1 {
		t.Fatalf("uninstall state=%#v deletes=%d error=%v", state, helm.deletes, err)
	}
}
