package radiusdeployment

import (
	"context"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/infrastructure/runtimeslot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ScaleAdapter struct {
	client kubernetes.Interface
	scale  *runtimeslot.ScaleClient
}

func NewScaleAdapter(client kubernetes.Interface) (*ScaleAdapter, error) {
	scale, err := runtimeslot.NewScaleClient(client)
	if err != nil {
		return nil, err
	}
	return &ScaleAdapter{client: client, scale: scale}, nil
}
func target(slot domain.Slot) runtimeslot.Slot {
	return runtimeslot.Slot{WorkloadKind: slot.WorkloadKind, Namespace: slot.Namespace, Name: slot.Name, UID: slot.UID, TemplateDigest: slot.TemplateDigest, MaxReplicas: slot.MaxReplicas}
}
func (a *ScaleAdapter) Observe(ctx context.Context, slot domain.Slot) (domain.Observation, error) {
	if err := domain.ValidateSlot(slot); err != nil {
		return domain.Observation{}, err
	}
	accountCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	account, err := a.serviceAccount(accountCtx, slot)
	cancel()
	if err != nil {
		return domain.Observation{}, err
	}
	if account != slot.ServiceAccount {
		return domain.Observation{}, fmt.Errorf("%w: runtime_service_account_changed", apperrors.ErrConflict)
	}
	o, err := a.scale.Observe(ctx, target(slot))
	return domain.Observation{UID: o.UID, TemplateDigest: o.TemplateDigest, ResourceVersion: o.ResourceVersion, Replicas: o.Replicas, DesiredReplicas: o.DesiredReplicas, ReadyReplicas: o.ReadyReplicas, ObservedAt: o.ObservedAt}, err
}
func (a *ScaleAdapter) Scale(ctx context.Context, slot domain.Slot, observed domain.Observation, replicas int32) error {
	return a.scale.Scale(ctx, target(slot), runtimeslot.Observation{UID: observed.UID, TemplateDigest: observed.TemplateDigest, ResourceVersion: observed.ResourceVersion, Replicas: observed.Replicas, DesiredReplicas: observed.DesiredReplicas, ReadyReplicas: observed.ReadyReplicas, ObservedAt: observed.ObservedAt}, replicas)
}
func (a *ScaleAdapter) serviceAccount(ctx context.Context, slot domain.Slot) (string, error) {
	if slot.WorkloadKind == "StatefulSet" {
		item, err := a.client.AppsV1().StatefulSets(slot.Namespace).Get(ctx, slot.Name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return item.Spec.Template.Spec.ServiceAccountName, nil
	}
	item, err := a.client.AppsV1().Deployments(slot.Namespace).Get(ctx, slot.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return item.Spec.Template.Spec.ServiceAccountName, nil
}
