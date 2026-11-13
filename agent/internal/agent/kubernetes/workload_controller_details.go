package kubernetes

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func (c *Client) GetReplicaSetDetail(ctx context.Context, namespace, name string) (domainresource.ReplicaSetDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	item, err := c.typed.AppsV1().ReplicaSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ReplicaSetDetailView{}, err
	}
	pods, relations, err := c.loadWorkloadDetailRelations(queryCtx, namespace, item.Spec.Selector, item.OwnerReferences, item.Spec.Template, item.UID)
	if err != nil {
		return domainresource.ReplicaSetDetailView{}, err
	}
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicaSetDetailView{
		Name: item.Name, Namespace: item.Namespace, DesiredReplicas: desired,
		ReadyReplicas: item.Status.ReadyReplicas, AvailableReplicas: item.Status.AvailableReplicas,
		CreatedAt: item.CreationTimestamp.Time.Format(time.RFC3339), Labels: item.Labels,
		Annotations: item.Annotations, Selector: item.Spec.Selector.MatchLabels, Pods: pods,
		RelatedResources: relations,
	}, nil
}

func (c *Client) GetReplicationControllerDetail(ctx context.Context, namespace, name string) (domainresource.ReplicationControllerDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	item, err := c.typed.CoreV1().ReplicationControllers(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ReplicationControllerDetailView{}, err
	}
	template := corev1.PodTemplateSpec{}
	if item.Spec.Template != nil {
		template = *item.Spec.Template
	}
	selector := &metav1.LabelSelector{MatchLabels: item.Spec.Selector}
	pods, relations, err := c.loadWorkloadDetailRelations(queryCtx, namespace, selector, item.OwnerReferences, template, item.UID)
	if err != nil {
		return domainresource.ReplicationControllerDetailView{}, err
	}
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicationControllerDetailView{
		Name: item.Name, Namespace: item.Namespace, DesiredReplicas: desired,
		CurrentReplicas: item.Status.Replicas, ReadyReplicas: item.Status.ReadyReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		CreatedAt:         item.CreationTimestamp.Time.Format(time.RFC3339), Labels: item.Labels,
		Annotations: item.Annotations, Selector: item.Spec.Selector, Pods: pods,
		RelatedResources: relations,
	}, nil
}
