package delivery

import (
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestApplicationRuntimeSummaryForAggregatesHealthAndServiceContext(t *testing.T) {
	environments := []domaindelivery.ApplicationRuntimeEnvironment{{
		Workloads: []domaindelivery.ApplicationRuntimeWorkload{
			{WorkloadName: "api-deployment", Labels: map[string]string{"serviceKey": "api"}, DesiredReplicas: 2, ReadyReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2},
			{WorkloadName: "worker", DesiredReplicas: 1},
		},
	}}
	services := []domainapp.Service{{ID: "service-1", Key: "api", Name: "API"}}

	summary := applicationRuntimeSummaryFor(environments, services)

	if summary.ServiceCount != 1 || summary.EnvironmentCount != 1 || summary.WorkloadCount != 2 {
		t.Fatalf("summary counts = %+v", summary)
	}
	if summary.HealthyWorkloadCount != 1 || summary.UnhealthyWorkloads != 1 || summary.HealthStatus != runtimeHealthUnhealthy {
		t.Fatalf("summary health = %+v", summary)
	}
	if got := environments[0].Workloads[0]; got.ServiceID != "service-1" || got.ServiceKey != "api" || got.HealthStatus != runtimeHealthHealthy {
		t.Fatalf("matched workload = %+v", got)
	}
}

func TestRuntimeWorkloadHealthTreatsPartialRolloutAsProgressing(t *testing.T) {
	got := runtimeWorkloadHealth(domaindelivery.ApplicationRuntimeWorkload{
		DesiredReplicas: 3, ReadyReplicas: 1, UpdatedReplicas: 2, AvailableReplicas: 1,
	})
	if got != runtimeHealthProgressing {
		t.Fatalf("runtimeWorkloadHealth = %q, want %q", got, runtimeHealthProgressing)
	}
}
