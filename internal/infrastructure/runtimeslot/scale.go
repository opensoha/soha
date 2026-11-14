// Package runtimeslot observes and scales installer-pinned Kubernetes Deployments.
package runtimeslot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

// Slot contains only the Kubernetes identity and scale ceiling approved by the installer.
// Lifecycle ownership, executor authorization and adoption remain with the caller.
type Slot struct {
	WorkloadKind   string
	Namespace      string
	Name           string
	UID            string
	TemplateDigest string
	MaxReplicas    int32
}

// Observation pins a scale write to the Deployment snapshot that was validated.
type Observation struct {
	UID             string
	ResourceVersion string
	TemplateDigest  string
	Replicas        int32
	DesiredReplicas int32
	ReadyReplicas   int32
	ObservedAt      time.Time
}

type ScaleClient struct{ client kubernetes.Interface }

func NewScaleClient(client kubernetes.Interface) (*ScaleClient, error) {
	if client == nil {
		return nil, fmt.Errorf("runtime scale client is required")
	}
	return &ScaleClient{client: client}, nil
}

// ValidateSlot checks the immutable target coordinates before Kubernetes access.
func ValidateSlot(slot Slot) error {
	if (slot.WorkloadKind != "" && slot.WorkloadKind != "Deployment" && slot.WorkloadKind != "StatefulSet") || len(validation.IsDNS1123Label(slot.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(slot.Name)) != 0 || slot.UID == "" || slot.TemplateDigest == "" {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

// Observe checks both identity and immutable template before exposing actual
// replicas. Only the scale subresource is ever writable by this adapter.
func (s *ScaleClient) Observe(ctx context.Context, slot Slot) (Observation, error) {
	if err := ValidateSlot(slot); err != nil {
		return Observation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployments := s.scaleTarget(slot)
	item, template, replicas, ready, err := s.snapshot(ctx, slot)
	if err != nil {
		return Observation{}, fmt.Errorf("read runtime Deployment: %w", err)
	}
	encoded, err := json.Marshal(template)
	if err != nil {
		return Observation{}, err
	}
	digest := sha256.Sum256(encoded)
	templateDigest := "sha256:" + hex.EncodeToString(digest[:])
	if string(item.UID) != slot.UID || templateDigest != slot.TemplateDigest || item.DeletionTimestamp != nil {
		return Observation{}, fmt.Errorf("%w: runtime workload identity or template changed", apperrors.ErrConflict)
	}
	scale, err := deployments.GetScale(ctx, slot.Name, metav1.GetOptions{})
	if err != nil {
		return Observation{}, fmt.Errorf("read runtime scale: %w", err)
	}
	if string(scale.UID) != slot.UID || scale.ResourceVersion == "" {
		return Observation{}, apperrors.ErrConflict
	}
	if item.ResourceVersion != scale.ResourceVersion {
		return Observation{}, fmt.Errorf("%w: runtime workload changed during observation; observe again", apperrors.ErrConflict)
	}
	return Observation{UID: slot.UID, TemplateDigest: templateDigest, ResourceVersion: scale.ResourceVersion, DesiredReplicas: scale.Spec.Replicas, Replicas: replicas, ReadyReplicas: ready, ObservedAt: time.Now().UTC()}, nil
}

func (s *ScaleClient) Scale(ctx context.Context, slot Slot, observation Observation, replicas int32) error {
	if err := ValidateSlot(slot); err != nil {
		return err
	}
	if replicas < 0 || replicas > slot.MaxReplicas || observation.UID != slot.UID || observation.TemplateDigest != slot.TemplateDigest || observation.ResourceVersion == "" {
		return apperrors.ErrAccessDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployments := s.scaleTarget(slot)
	scale, err := deployments.GetScale(ctx, slot.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(scale.UID) != slot.UID || scale.ResourceVersion != observation.ResourceVersion {
		return apperrors.ErrConflict
	}
	scale.Spec.Replicas = replicas
	updated, err := deployments.UpdateScale(ctx, slot.Name, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("write runtime scale; read back before retry: %w", err)
	}
	if updated == nil || string(updated.UID) != slot.UID || updated.Spec.Replicas != replicas {
		return fmt.Errorf("%w: scale result requires observation", apperrors.ErrConflict)
	}
	return nil
}

type scaleTarget interface {
	GetScale(context.Context, string, metav1.GetOptions) (*autoscalingv1.Scale, error)
	UpdateScale(context.Context, string, *autoscalingv1.Scale, metav1.UpdateOptions) (*autoscalingv1.Scale, error)
}

func (s *ScaleClient) scaleTarget(slot Slot) scaleTarget {
	if slot.WorkloadKind == "StatefulSet" {
		return s.client.AppsV1().StatefulSets(slot.Namespace)
	}
	return s.client.AppsV1().Deployments(slot.Namespace)
}
func (s *ScaleClient) snapshot(ctx context.Context, slot Slot) (metav1.ObjectMeta, corev1.PodTemplateSpec, int32, int32, error) {
	if slot.WorkloadKind == "StatefulSet" {
		item, err := s.client.AppsV1().StatefulSets(slot.Namespace).Get(ctx, slot.Name, metav1.GetOptions{})
		if err != nil {
			return metav1.ObjectMeta{}, corev1.PodTemplateSpec{}, 0, 0, err
		}
		if item.Spec.PodManagementPolicy != appsv1.ParallelPodManagement || (item.Spec.Ordinals != nil && item.Spec.Ordinals.Start != 0) {
			return metav1.ObjectMeta{}, corev1.PodTemplateSpec{}, 0, 0, apperrors.ErrConflict
		}
		if p := item.Spec.PersistentVolumeClaimRetentionPolicy; p != nil && (p.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType || p.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType) {
			return metav1.ObjectMeta{}, corev1.PodTemplateSpec{}, 0, 0, apperrors.ErrConflict
		}
		return item.ObjectMeta, item.Spec.Template, item.Status.Replicas, item.Status.ReadyReplicas, nil
	}
	item, err := s.client.AppsV1().Deployments(slot.Namespace).Get(ctx, slot.Name, metav1.GetOptions{})
	if err != nil {
		return metav1.ObjectMeta{}, corev1.PodTemplateSpec{}, 0, 0, err
	}
	return item.ObjectMeta, item.Spec.Template, item.Status.Replicas, item.Status.ReadyReplicas, nil
}
