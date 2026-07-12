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

	assertWorkloadOverviewSummary(t, view)
	assertWorkloadOverviewRiskDetails(t, view)
}

func assertWorkloadOverviewSummary(t *testing.T, view domainresource.WorkloadOverviewView) {
	t.Helper()
	stringCases := []struct {
		name string
		got  string
		want string
	}{
		{name: "ClusterID", got: view.ClusterID, want: "cluster-a"},
		{name: "Namespace", got: view.Namespace, want: ""},
		{name: "Source", got: view.Source, want: "live"},
	}
	for _, testCase := range stringCases {
		if testCase.got != testCase.want {
			t.Errorf("%s = %q, want %q", testCase.name, testCase.got, testCase.want)
		}
	}
	countCases := []struct {
		name string
		got  int
		want int
	}{
		{name: "TotalPods", got: view.TotalPods, want: 5},
		{name: "RunningPods", got: view.RunningPods, want: 2},
		{name: "PendingPods", got: view.PendingPods, want: 1},
		{name: "SucceededPods", got: view.SucceededPods, want: 1},
		{name: "FailedPods", got: view.FailedPods, want: 1},
		{name: "RestartingPods", got: view.RestartingPods, want: 2},
		{name: "AtRiskPods", got: view.AtRiskPods, want: 3},
	}
	for _, testCase := range countCases {
		if testCase.got != testCase.want {
			t.Errorf("%s = %d, want %d", testCase.name, testCase.got, testCase.want)
		}
	}
}

func assertWorkloadOverviewRiskDetails(t *testing.T, view domainresource.WorkloadOverviewView) {
	t.Helper()
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
