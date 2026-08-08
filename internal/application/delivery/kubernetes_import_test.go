package delivery

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestListKubernetesImportCandidatesGroupsRelatedResources(t *testing.T) {
	t.Parallel()
	key := "cluster-a/team-a"
	targets := stubTargetReader{
		deployments:       map[string][]domainresource.DeploymentView{key: {{Name: "api", Namespace: "team-a", Labels: map[string]string{"app": "api"}, DesiredReplicas: 3, ReadyReplicas: 2}}},
		deploymentDetails: map[string]domainresource.DeploymentDetailView{"cluster-a/team-a/api": {Containers: []domainresource.WorkloadContainerView{{Name: "api"}}}},
		statefulSets:      map[string][]domainresource.StatefulSetView{key: {{Name: "db", Namespace: "team-a", ServiceName: "db", DesiredReplicas: 1, ReadyReplicas: 1}}},
		daemonSets:        map[string][]domainresource.DaemonSetView{key: {{Name: "node-agent", Namespace: "team-a", DesiredNumber: 4, ReadyNumber: 3}}},
		services: map[string][]domainresource.ServiceView{key: {
			{Name: "api-service", Namespace: "team-a", Selector: map[string]string{"app": "api"}},
			{Name: "db", Namespace: "team-a"},
		}},
		ingresses: map[string][]domainresource.IngressView{key: {{Name: "public", Namespace: "team-a", BackendServices: []string{"api-service"}}}},
		hpas:      map[string][]domainresource.HorizontalPodAutoscalerView{key: {{Name: "api-hpa", Namespace: "team-a", TargetRef: "Deployment/api"}}},
	}
	service := New(stubApplicationReader{}, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, stubRepository{}, nil, targets,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationEnvView))

	page, err := service.ListKubernetesImportCandidates(context.Background(), deliveryActionPrincipal(), domaindelivery.KubernetesImportCandidateFilter{
		ClusterID: " cluster-a ", Namespace: " team-a ", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Truncated || len(page.Items) != 2 {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0].WorkloadKind != "Deployment" || page.Items[0].WorkloadName != "api" {
		t.Fatalf("first candidate = %#v", page.Items[0])
	}
	if len(page.Items[0].Containers) != 1 || page.Items[0].Containers[0] != "api" {
		t.Fatalf("containers = %#v", page.Items[0].Containers)
	}
	wantRelations := []domaindelivery.KubernetesRelatedResource{
		{Kind: "HorizontalPodAutoscaler", Name: "api-hpa"},
		{Kind: "Ingress", Name: "public"},
		{Kind: "Service", Name: "api-service"},
	}
	if len(page.Items[0].RelatedResources) != len(wantRelations) {
		t.Fatalf("relations = %#v", page.Items[0].RelatedResources)
	}
	for index := range wantRelations {
		if page.Items[0].RelatedResources[index] != wantRelations[index] {
			t.Fatalf("relations = %#v", page.Items[0].RelatedResources)
		}
	}
}

func TestImportKubernetesServicesValidatesOwnershipAndLiveWorkloads(t *testing.T) {
	t.Parallel()
	key := "cluster-a/team-a"
	targets := stubTargetReader{deployments: map[string][]domainresource.DeploymentView{key: {{Name: "api", Namespace: "team-a"}}}}
	var captured domaindelivery.KubernetesServiceImportInput
	calls := 0
	repo := stubRepository{importInput: &captured, importCallCount: &calls, importResult: domaindelivery.KubernetesServiceImportResult{OwnershipMode: "observe_only"}}
	service := New(stubApplicationReader{}, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, targets,
		deliveryActionPermissions(
			appaccess.PermDeliveryApplicationEnvView,
			appaccess.PermDeliveryApplicationsCreate,
			appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationServicesManage, "create"),
			appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "create"),
		))

	input := domaindelivery.KubernetesServiceImportInput{
		ClusterID: " cluster-a ", Namespace: " team-a ", ApplicationKey: " payments ", ApplicationName: " Payments ",
		EnvironmentKey: " prod ", EnvironmentName: " Production ", OwnershipMode: "observe_only",
		Workloads: []domaindelivery.KubernetesWorkloadImportInput{{WorkloadKind: "Deployment", WorkloadName: "api"}},
	}
	if _, err := service.ImportKubernetesServices(context.Background(), deliveryActionPrincipal(), input); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || captured.ClusterID != "cluster-a" || captured.Namespace != "team-a" || captured.ApplicationKey != "payments" {
		t.Fatalf("captured = %#v, calls = %d", captured, calls)
	}

	input.OwnershipMode = "managed"
	if _, err := service.ImportKubernetesServices(context.Background(), deliveryActionPrincipal(), input); err != nil {
		t.Fatalf("managed Deployment import error = %v", err)
	}
	input.OwnershipMode = "observe_only"
	input.Workloads[0].WorkloadName = "missing"
	if _, err := service.ImportKubernetesServices(context.Background(), deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing workload error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("repository called %d times", calls)
	}
}

func TestManagedKubernetesImportRejectsNonDeployment(t *testing.T) {
	t.Parallel()
	input := domaindelivery.KubernetesServiceImportInput{
		ClusterID: "cluster-a", Namespace: "team-a", ApplicationKey: "payments", ApplicationName: "Payments",
		EnvironmentKey: "prod", EnvironmentName: "Production", OwnershipMode: "managed",
		Workloads: []domaindelivery.KubernetesWorkloadImportInput{{WorkloadKind: "StatefulSet", WorkloadName: "db"}},
	}
	if err := validateKubernetesServiceImportInput(input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("validateKubernetesServiceImportInput() error = %v", err)
	}
}

func TestImportHelmReleasesValidatesLiveReleaseAndCapturesMetadata(t *testing.T) {
	t.Parallel()
	key := "cluster-a/team-a"
	targets := stubTargetReader{helmReleases: map[string][]domainresource.HelmReleaseView{key: {{
		Name: "payments", Namespace: "team-a", Revision: "7", Status: "deployed", Chart: "payments-1.4.0", AppVersion: "2026.08", StorageDriver: "secret",
	}}}}
	var captured domaindelivery.HelmReleaseImportInput
	calls := 0
	repo := stubRepository{
		helmImportInput: &captured, helmImportCallCount: &calls,
		helmImportResult: domaindelivery.HelmReleaseImportResult{OwnershipMode: "managed"},
	}
	service := New(stubApplicationReader{}, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, targets,
		deliveryActionPermissions(
			appaccess.PermDeliveryApplicationEnvView,
			appaccess.PermDeliveryApplicationsCreate,
			appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationServicesManage, "create"),
			appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "create"),
		))
	input := domaindelivery.HelmReleaseImportInput{
		ClusterID: " cluster-a ", Namespace: " team-a ", ApplicationKey: " payments ", ApplicationName: " Payments ",
		EnvironmentKey: " prod ", EnvironmentName: " Production ", OwnershipMode: "managed",
		Releases: []domaindelivery.HelmReleaseImportItem{{ReleaseName: "payments"}},
	}
	if _, err := service.ImportHelmReleases(context.Background(), deliveryActionPrincipal(), input); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || captured.ClusterID != "cluster-a" || captured.Releases[0].Chart != "payments-1.4.0" || captured.Releases[0].Revision != "7" {
		t.Fatalf("captured = %#v, calls = %d", captured, calls)
	}

	input.Releases[0].ReleaseName = "missing"
	if _, err := service.ImportHelmReleases(context.Background(), deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing release error = %v", err)
	}
}
