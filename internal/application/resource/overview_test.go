package resource

import (
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestBuildWorkloadOverviewAggregatesPodRiskByScope(t *testing.T) {
	t.Parallel()

	view := buildWorkloadOverview("cluster-a", " ", "live", []domainresource.PodView{
		{Name: "api", Namespace: "team-a", Phase: "Running", ReadyContainers: "1/1", Restarts: 2, NodeName: "node-1", AgeSeconds: 240},
		{Name: "worker", Namespace: "team-a", Phase: "Running", ReadyContainers: "1/1", Restarts: 0, NodeName: "node-2", AgeSeconds: 120},
		{Name: "job-ok", Namespace: "team-b", Phase: "Succeeded", ReadyContainers: "0/1", Restarts: 0, NodeName: "node-3", AgeSeconds: 90},
		{Name: "pending", Namespace: "team-b", Phase: "Pending", ReadyContainers: "0/1", Restarts: 0, NodeName: "node-4", AgeSeconds: 60},
		{Name: "failing", Namespace: "team-b", Phase: "Failed", ReadyContainers: "0/1", Restarts: 1, NodeName: "node-5", AgeSeconds: 30},
	})

	if view.ClusterID != "cluster-a" {
		t.Fatalf("ClusterID = %q, want cluster-a", view.ClusterID)
	}
	if view.Namespace != "" {
		t.Fatalf("Namespace = %q, want empty", view.Namespace)
	}
	if view.Source != "live" {
		t.Fatalf("Source = %q, want live", view.Source)
	}
	if view.TotalPods != 5 {
		t.Fatalf("TotalPods = %d, want 5", view.TotalPods)
	}
	if view.RunningPods != 2 {
		t.Fatalf("RunningPods = %d, want 2", view.RunningPods)
	}
	if view.PendingPods != 1 {
		t.Fatalf("PendingPods = %d, want 1", view.PendingPods)
	}
	if view.SucceededPods != 1 {
		t.Fatalf("SucceededPods = %d, want 1", view.SucceededPods)
	}
	if view.FailedPods != 1 {
		t.Fatalf("FailedPods = %d, want 1", view.FailedPods)
	}
	if view.RestartingPods != 2 {
		t.Fatalf("RestartingPods = %d, want 2", view.RestartingPods)
	}
	if view.AtRiskPods != 3 {
		t.Fatalf("AtRiskPods = %d, want 3", view.AtRiskPods)
	}
	if len(view.NamespaceBreakdown) != 2 {
		t.Fatalf("len(NamespaceBreakdown) = %d, want 2", len(view.NamespaceBreakdown))
	}
	if got := view.NamespaceBreakdown[0]; got.Namespace != "team-b" || got.AtRiskPods != 2 || got.TotalPods != 3 {
		t.Fatalf("NamespaceBreakdown[0] = %+v, want team-b with 2 risky pods and 3 total pods", got)
	}
	if got := view.NamespaceBreakdown[1]; got.Namespace != "team-a" || got.AtRiskPods != 1 || got.RestartingPods != 1 {
		t.Fatalf("NamespaceBreakdown[1] = %+v, want team-a with 1 risky pod and 1 restarting pod", got)
	}
	if len(view.ProblematicPods) != 3 {
		t.Fatalf("len(ProblematicPods) = %d, want 3", len(view.ProblematicPods))
	}
	if got := view.ProblematicPods[0]; got.Name != "api" || got.Restarts != 2 {
		t.Fatalf("ProblematicPods[0] = %+v, want api with 2 restarts", got)
	}
	if got := view.ProblematicPods[2]; got.Name != "pending" || got.Phase != "Pending" {
		t.Fatalf("ProblematicPods[2] = %+v, want pending pod", got)
	}
}

func TestPodNeedsAttentionIgnoresHealthyCompletedPods(t *testing.T) {
	t.Parallel()

	if podNeedsAttention(domainresource.PodView{Phase: "Succeeded"}) {
		t.Fatal("podNeedsAttention(Succeeded) = true, want false")
	}
	if !podNeedsAttention(domainresource.PodView{Phase: "Failed"}) {
		t.Fatal("podNeedsAttention(Failed) = false, want true")
	}
	if !podNeedsAttention(domainresource.PodView{Phase: "Running", Restarts: 1}) {
		t.Fatal("podNeedsAttention(Running with restarts) = false, want true")
	}
}
