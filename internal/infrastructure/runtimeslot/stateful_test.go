package runtimeslot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"testing"
)

func TestStatefulSlotUsesOnlyPinnedStatefulScaleAndRequiresParallelRetain(t *testing.T) {
	for _, name := range []string{"approved", "ordered", "ordinal offset", "delete PVC", "replacement UID", "changed template"} {
		t.Run(name, func(t *testing.T) {
			item := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "soha", UID: "uid", ResourceVersion: "rv"}, Spec: appsv1.StatefulSetSpec{PodManagementPolicy: appsv1.ParallelPodManagement, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "runtime", Image: "approved"}}}}}, Status: appsv1.StatefulSetStatus{Replicas: 1, ReadyReplicas: 1}}
			raw, _ := json.Marshal(item.Spec.Template)
			slot := Slot{WorkloadKind: "StatefulSet", Namespace: "soha", Name: "pool", UID: "uid", TemplateDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), MaxReplicas: 2}
			switch name {
			case "ordered":
				item.Spec.PodManagementPolicy = appsv1.OrderedReadyPodManagement
			case "ordinal offset":
				item.Spec.Ordinals = &appsv1.StatefulSetOrdinals{Start: 1}
			case "delete PVC":
				item.Spec.PersistentVolumeClaimRetentionPolicy = &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType, WhenScaled: appsv1.RetainPersistentVolumeClaimRetentionPolicyType}
			case "replacement UID":
				item.UID = "replacement"
			case "changed template":
				item.Spec.Template.Spec.Containers[0].Image = "other"
			}
			client := fake.NewClientset(item)
			writes := 0
			client.PrependReactor("get", "statefulsets", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() == "scale" {
					return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: "pool", UID: "uid", ResourceVersion: "rv"}, Spec: autoscalingv1.ScaleSpec{Replicas: 1}}, nil
				}
				return false, nil, nil
			})
			client.PrependReactor("update", "statefulsets", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				if action.GetSubresource() != "scale" {
					t.Fatal("wrote stateful spec")
				}
				update, ok := action.(ktesting.UpdateAction)
				if !ok || update == nil {
					t.Fatalf("unexpected update action %T", action)
				}
				return true, update.GetObject(), nil
			})
			scaler, _ := NewScaleClient(client)
			observed, err := scaler.Observe(context.Background(), slot)
			if name != "approved" {
				if !errors.Is(err, apperrors.ErrConflict) || writes != 0 {
					t.Fatalf("accepted unsafe slot %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = scaler.Scale(context.Background(), slot, observed, 2); err != nil || writes != 1 {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
		})
	}
}
