package resource

import (
	"context"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"strings"
)

func (w *Workloads) listControllerRolloutHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, kind string) ([]domainresource.RolloutHistoryView, error) {
	agentRead := WorkloadAgent.ListStatefulSetRolloutHistory
	if kind == "DaemonSet" {
		agentRead = WorkloadAgent.ListDaemonSetRolloutHistory
	}
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[[]domainresource.RolloutHistoryView]{kind: kind, auditText: "viewed " + strings.ToLower(kind) + " revision history",
		agent: func(client WorkloadAgent) ([]domainresource.RolloutHistoryView, error) {
			return agentRead(client, ctx, namespace, name)
		},
		direct: func() ([]domainresource.RolloutHistoryView, string, error) {
			return liveWorkload(func() ([]domainresource.RolloutHistoryView, error) {
				if kind == "DaemonSet" {
					return w.direct.ListDaemonSetRolloutHistory(ctx, clusterID, namespace, name)
				}
				return w.direct.ListStatefulSetRolloutHistory(ctx, clusterID, namespace, name)
			})
		},
	})
}
func (w *Workloads) getControllerRevisionYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revisionName, kind string) (domainresource.ResourceYAMLView, error) {
	agentRead := WorkloadAgent.GetStatefulSetRevisionYAML
	if kind == "DaemonSet" {
		agentRead = WorkloadAgent.GetDaemonSetRevisionYAML
	}
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ResourceYAMLView]{kind: kind, auditText: "viewed " + strings.ToLower(kind) + " revision yaml",
		agent: func(client WorkloadAgent) (domainresource.ResourceYAMLView, error) {
			return agentRead(client, ctx, namespace, name, revisionName)
		},
		direct: func() (domainresource.ResourceYAMLView, string, error) {
			return liveWorkload(func() (domainresource.ResourceYAMLView, error) {
				if kind == "DaemonSet" {
					return w.direct.GetDaemonSetRevisionYAML(ctx, clusterID, namespace, name, revisionName)
				}
				return w.direct.GetStatefulSetRevisionYAML(ctx, clusterID, namespace, name, revisionName)
			})
		},
	})
}
