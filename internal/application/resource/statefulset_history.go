package resource

import (
	"context"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (w *Workloads) ListStatefulSetRolloutHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	return w.listControllerRolloutHistory(ctx, principal, clusterID, namespace, name, "StatefulSet")
}
func (w *Workloads) GetStatefulSetRevisionYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	return w.getControllerRevisionYAML(ctx, principal, clusterID, namespace, name, revisionName, "StatefulSet")
}
