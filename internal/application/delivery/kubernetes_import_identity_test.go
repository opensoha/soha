package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func identityImportInput() domaindelivery.KubernetesServiceImportInput {
	return domaindelivery.KubernetesServiceImportInput{ClusterID: "cluster", Namespace: "team", ApplicationKey: "app", ApplicationName: "App", EnvironmentKey: "test", EnvironmentName: "Test", OwnershipMode: "observe_only", Workloads: []domaindelivery.KubernetesWorkloadImportInput{{WorkloadKind: "Deployment", WorkloadName: "api", ExpectedUID: "uid-1", ExpectedSpecDigest: strings.Repeat("a", 64)}}}
}

func identityImportSource(kind string) domainresource.ResourceSourceSnapshot {
	return domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: "cluster", Namespace: "team", APIVersion: "apps/v1", Kind: kind, Name: "api", UID: "uid-1"}, SpecDigest: strings.Repeat("a", 64)}
}

func identityImportService(app stubApplicationReader, catalog stubCatalogReader, targets stubTargetReader, repo stubRepository) *Service {
	return New(app, catalog, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, targets, deliveryActionPermissions(appaccess.PermDeliveryApplicationEnvView, appaccess.PermDeliveryApplicationsCreate, appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationServicesManage, "create")))
}

func TestImportRejectsDriftAndScopeBeforePersistence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                     string
		uid, digest                              string
		appErr, bindingErr, sourceErr, manageErr error
		managed, external                        bool
		want                                     error
	}{
		{name: "uid replacement", uid: "other", want: apperrors.ErrConflict},
		{name: "configuration drift", digest: strings.Repeat("b", 64), want: apperrors.ErrConflict},
		{name: "application denied", appErr: apperrors.ErrAccessDenied, want: apperrors.ErrAccessDenied},
		{name: "binding denied", bindingErr: apperrors.ErrAccessDenied, want: apperrors.ErrAccessDenied},
		{name: "source denied", sourceErr: apperrors.ErrAccessDenied, want: apperrors.ErrAccessDenied},
		{name: "managed update denied", managed: true, manageErr: apperrors.ErrAccessDenied, want: apperrors.ErrAccessDenied},
		{name: "external manager", managed: true, external: true, want: apperrors.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, source := identityImportInput(), identityImportSource("Deployment")
			if tc.uid != "" {
				source.Resource.UID = tc.uid
			}
			if tc.digest != "" {
				source.SpecDigest = tc.digest
			}
			if tc.managed {
				input.OwnershipMode = "managed"
			}
			source.ExternallyManaged = tc.external
			calls := 0
			service := identityImportService(stubApplicationReader{importScopeErr: tc.appErr}, stubCatalogReader{importScopeErr: tc.bindingErr}, stubTargetReader{importSources: map[string]domainresource.ResourceSourceSnapshot{"Deployment/api": source}, importSourceErr: tc.sourceErr, importManagementErr: tc.manageErr}, stubRepository{importCallCount: &calls})
			_, err := service.ImportKubernetesServices(context.Background(), deliveryActionPrincipal(), input)
			if !errors.Is(err, tc.want) || calls != 0 {
				t.Fatalf("err=%v writes=%d", err, calls)
			}
		})
	}
}

func TestImportPinsAllKindsAndAuthorizesActualDestination(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "CustomResource"} {
		t.Run(kind, func(t *testing.T) {
			input, source := identityImportInput(), identityImportSource(kind)
			input.Workloads[0].WorkloadKind = kind
			if kind == "CustomResource" {
				input.Workloads[0].CRDName = "widgets.example.com"
				source.CRDName = "widgets.example.com"
				source.Plural = "widgets"
				source.Resource.Kind = "Widget"
				source.Resource.APIVersion = "example.com/v1"
			}
			var captured domaindelivery.KubernetesServiceImportInput
			var appID, bindingID string
			destination := domaindelivery.ImportDestination{ApplicationID: "existing-app", EnvironmentID: "existing-env", BindingID: "existing-binding"}
			service := identityImportService(stubApplicationReader{importScopeID: &appID}, stubCatalogReader{importScopeID: &bindingID}, stubTargetReader{importSources: map[string]domainresource.ResourceSourceSnapshot{kind + "/api": source}}, stubRepository{importInput: &captured, importDestination: destination})
			// Existing application import does not require permission to create unrelated applications.
			service.permissions = deliveryActionPermissions(appaccess.PermDeliveryApplicationEnvView, appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationServicesManage, "create"))
			if _, err := service.ImportKubernetesServices(context.Background(), deliveryActionPrincipal(), input); err != nil {
				t.Fatal(err)
			}
			if appID != destination.ApplicationID || bindingID != destination.BindingID || captured.Destination != destination || captured.Workloads[0].Source.Resource.UID != "uid-1" {
				t.Fatalf("wrong scope/source: %+v %s %s", captured, appID, bindingID)
			}
		})
	}
}

func TestCronJobChildrenCannotBecomeIndependentImportServices(t *testing.T) {
	input, source := identityImportInput(), identityImportSource("Job")
	input.Workloads[0].WorkloadKind = "Job"
	controller := true
	source.Owners = []domainresource.ResourceOwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: "schedule", UID: "cron-uid", Controller: &controller}}
	service := identityImportService(stubApplicationReader{}, stubCatalogReader{}, stubTargetReader{importSources: map[string]domainresource.ResourceSourceSnapshot{"Job/api": source}}, stubRepository{})
	if _, err := service.ImportKubernetesServices(context.Background(), deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("child import=%v", err)
	}
}

func TestImportPreviewRequiresPairedIdentityAndIgnoresClientSnapshots(t *testing.T) {
	for _, workload := range []domaindelivery.KubernetesWorkloadImportInput{
		{WorkloadKind: "Deployment", ExpectedUID: "uid"},
		{WorkloadKind: "Deployment", ExpectedSpecDigest: strings.Repeat("a", 64)},
		{WorkloadKind: "Job"},
		{WorkloadKind: "CustomResource", CRDName: "widgets.example.com"},
		{WorkloadKind: "Deployment", CRDName: "widgets.example.com"},
	} {
		if err := validateImportPreview(workload); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("accepted invalid preview: %+v", workload)
		}
	}
	var item domaindelivery.KubernetesWorkloadImportInput
	if err := json.Unmarshal([]byte(`{"workloadKind":"Deployment","workloadName":"api","source":{"resource":{"uid":"forged"}}}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.Source != nil {
		t.Fatal("trusted client snapshot")
	}
}
