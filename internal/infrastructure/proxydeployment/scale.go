package proxydeployment

import (
	"context"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/infrastructure/runtimeslot"
	"k8s.io/client-go/kubernetes"
)

type ScaleClient struct{ client *runtimeslot.ScaleClient }

func NewScaleClient(client kubernetes.Interface) (*ScaleClient, error) {
	if client == nil {
		return nil, fmt.Errorf("proxy scale client is required")
	}
	scaler, err := runtimeslot.NewScaleClient(client)
	if err != nil {
		return nil, err
	}
	return &ScaleClient{client: scaler}, nil
}

func scaleSlot(slot domain.Slot) runtimeslot.Slot {
	return runtimeslot.Slot{WorkloadKind: slot.WorkloadKind, Namespace: slot.Namespace, Name: slot.Name, UID: slot.UID,
		TemplateDigest: slot.TemplateDigest, MaxReplicas: slot.MaxReplicas}
}

func validateSlot(slot domain.Slot) error {
	return runtimeslot.ValidateSlot(scaleSlot(slot))
}

func (s *ScaleClient) Observe(ctx context.Context, slot domain.Slot) (domain.Observation, error) {
	observed, err := s.client.Observe(ctx, scaleSlot(slot))
	if err != nil {
		return domain.Observation{}, err
	}
	return domain.Observation{UID: observed.UID, ResourceVersion: observed.ResourceVersion,
		TemplateDigest: observed.TemplateDigest, Replicas: observed.Replicas,
		DesiredReplicas: observed.DesiredReplicas, ReadyReplicas: observed.ReadyReplicas,
		ObservedAt: observed.ObservedAt}, nil
}

func (s *ScaleClient) Scale(ctx context.Context, slot domain.Slot, observation domain.Observation, replicas int32) error {
	return s.client.Scale(ctx, scaleSlot(slot), runtimeslot.Observation{
		UID: observation.UID, ResourceVersion: observation.ResourceVersion,
		TemplateDigest: observation.TemplateDigest, Replicas: observation.Replicas,
		DesiredReplicas: observation.DesiredReplicas, ReadyReplicas: observation.ReadyReplicas,
		ObservedAt: observation.ObservedAt}, replicas)
}
