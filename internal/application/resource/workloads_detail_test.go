package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type denyPodDetailAuthorizer struct{}

func (denyPodDetailAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: request.Resource.Kind != "Pod"}, nil
}

func TestPDBDetailRedactsRelationsWithoutPodAccess(t *testing.T) {
	t.Parallel()
	w := &Workloads{resourceAccess: &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}},
		authorizer: denyPodDetailAuthorizer{},
	}}
	item := domainresource.PodDisruptionBudgetDetailView{
		Pods:     []domainresource.PodView{{Name: "api-0", Namespace: "team-a"}},
		Workload: &domainresource.PodRelatedResourceView{Kind: "Deployment", Name: "api", Namespace: "team-a"},
	}
	got := w.redactPDBRelations(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", "team-a", item)
	if got.Pods != nil || got.Workload != nil {
		t.Fatalf("PDB relations were not redacted: %#v", got)
	}
}

func TestWorkloadDetailRedactsUnauthorizedRelations(t *testing.T) {
	t.Parallel()
	access := &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: relatedKindAuthorizer{
			"Pod": false, "Secret": false, "ConfigMap": true, "Job": false,
		},
	}
	principal := domainidentity.Principal{UserID: "user-1"}
	deployment := domainresource.DeploymentDetailView{
		Pods: []domainresource.PodView{{Name: "api-0", Namespace: "team-a"}},
		RelatedResources: []domainresource.WorkloadRelationView{
			{Kind: "Secret", Name: "private", Namespace: "team-a"},
			{Kind: "ConfigMap", Name: "public", Namespace: "team-a"},
		},
	}
	filterWorkloadDetailRelations(context.Background(), access, principal, "cluster-a", "team-a", &deployment)
	if deployment.Pods != nil || len(deployment.RelatedResources) != 1 || deployment.RelatedResources[0].Kind != "ConfigMap" {
		t.Fatalf("deployment relations were not redacted: %#v", deployment)
	}

	cronJob := domainresource.CronJobDetailView{Jobs: []domainresource.JobView{{Name: "private", Namespace: "team-a"}}}
	filterWorkloadDetailRelations(context.Background(), access, principal, "cluster-a", "team-a", &cronJob)
	if cronJob.Jobs != nil {
		t.Fatalf("cronjob jobs were not redacted: %#v", cronJob.Jobs)
	}
}
