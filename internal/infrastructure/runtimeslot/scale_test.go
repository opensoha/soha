package runtimeslot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func scaleFixture(t *testing.T) (*ScaleClient, *fake.Clientset, Slot) {
	t.Helper()
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "soha", UID: types.UID("uid"), ResourceVersion: "rv"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "outpost", Image: "image@sha256:approved"}}}}}, Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1}}
	encoded, _ := json.Marshal(deployment.Spec.Template)
	digest := sha256.Sum256(encoded)
	slot := Slot{Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "sha256:" + hex.EncodeToString(digest[:]), MaxReplicas: 3}
	client := fake.NewClientset(deployment)
	client.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "scale" {
			return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: "edge", UID: types.UID("uid"), ResourceVersion: "rv"}, Spec: autoscalingv1.ScaleSpec{Replicas: 1}}, nil
		}
		return false, nil, nil
	})
	scaler, _ := NewScaleClient(client)
	return scaler, client, slot
}

func TestScaleRejectsStaleOrUnapprovedWriteBeforeMutation(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*Slot, *Observation, *int32)
		scaleChange func(*autoscalingv1.Scale)
		want        error
	}{
		{name: "invalid namespace", change: func(s *Slot, _ *Observation, _ *int32) { s.Namespace = "../other" }, want: apperrors.ErrInvalidArgument},
		{name: "negative replicas", change: func(_ *Slot, _ *Observation, n *int32) { *n = -1 }, want: apperrors.ErrAccessDenied},
		{name: "replica ceiling", change: func(_ *Slot, _ *Observation, n *int32) { *n = 4 }, want: apperrors.ErrAccessDenied},
		{name: "observation uid", change: func(_ *Slot, o *Observation, _ *int32) { o.UID = "other" }, want: apperrors.ErrAccessDenied},
		{name: "observation template", change: func(_ *Slot, o *Observation, _ *int32) { o.TemplateDigest = "other" }, want: apperrors.ErrAccessDenied},
		{name: "missing resource version", change: func(_ *Slot, o *Observation, _ *int32) { o.ResourceVersion = "" }, want: apperrors.ErrAccessDenied},
		{name: "live uid changed", scaleChange: func(s *autoscalingv1.Scale) { s.UID = "replacement" }, want: apperrors.ErrConflict},
		{name: "live resource version changed", scaleChange: func(s *autoscalingv1.Scale) { s.ResourceVersion = "new-rv" }, want: apperrors.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scaler, client, slot := scaleFixture(t)
			observation, err := scaler.Observe(context.Background(), slot)
			if err != nil {
				t.Fatal(err)
			}
			replicas := int32(2)
			if tt.change != nil {
				tt.change(&slot, &observation, &replicas)
			}
			if tt.scaleChange != nil {
				client.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
					if action.GetSubresource() != "scale" {
						t.Fatal("unexpected Deployment read")
					}
					scale := &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{UID: "uid", ResourceVersion: "rv"}}
					tt.scaleChange(scale)
					return true, scale, nil
				})
			}
			if err := scaler.Scale(context.Background(), slot, observation, replicas); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatalf("unsafe mutation: %v", action)
				}
			}
		})
	}
}

func TestScaleUnknownWriteIsNotRetried(t *testing.T) {
	unavailable := fmt.Errorf("API response lost")
	for _, outcome := range []string{"transport error", "uid mismatch", "replica mismatch"} {
		t.Run(outcome, func(t *testing.T) {
			scaler, client, slot := scaleFixture(t)
			observation, err := scaler.Observe(context.Background(), slot)
			if err != nil {
				t.Fatal(err)
			}
			writes := 0
			client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				if action.GetSubresource() != "scale" || action.GetNamespace() != slot.Namespace {
					t.Fatalf("wrong scale target: %v", action)
				}
				scale := action.(ktesting.UpdateAction).GetObject().(*autoscalingv1.Scale).DeepCopy()
				if scale.Name != slot.Name || scale.UID != types.UID(slot.UID) || scale.ResourceVersion != observation.ResourceVersion || scale.Spec.Replicas != 2 {
					t.Fatalf("unfenced scale write: %+v", scale)
				}
				switch outcome {
				case "transport error":
					return true, nil, unavailable
				case "uid mismatch":
					scale.UID = "replacement"
				case "replica mismatch":
					scale.Spec.Replicas = 3
				}
				return true, scale, nil
			})
			err = scaler.Scale(context.Background(), slot, observation, 2)
			want := error(apperrors.ErrConflict)
			if outcome == "transport error" {
				want = unavailable
			}
			if !errors.Is(err, want) || writes != 1 {
				t.Fatalf("err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestObserveRejectsDeletingWorkload(t *testing.T) {
	scaler, client, slot := scaleFixture(t)
	client.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "" {
			t.Fatal("read scale after deleting Deployment")
		}
		obj, err := client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), slot.Namespace, slot.Name)
		if err != nil {
			return true, nil, err
		}
		deployment := obj.(*appsv1.Deployment).DeepCopy()
		at := metav1.Now()
		deployment.DeletionTimestamp = &at
		return true, deployment, nil
	})
	if _, err := scaler.Observe(context.Background(), slot); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal(err)
	}
}
