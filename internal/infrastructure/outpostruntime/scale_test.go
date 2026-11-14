package outpostruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
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

func scaleFixture(t *testing.T) (*ScaleClient, *fake.Clientset, domain.Slot) {
	t.Helper()
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "soha", UID: types.UID("uid"), ResourceVersion: "rv"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "outpost", Image: "image@sha256:approved"}}}}}, Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1}}
	encoded, _ := json.Marshal(deployment.Spec.Template)
	digest := sha256.Sum256(encoded)
	slot := domain.Slot{ID: "slot", OutpostID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "sha256:" + hex.EncodeToString(digest[:]), MaxReplicas: 3}
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

func TestObservationRejectsTemplateChangeBetweenReads(t *testing.T) {
	scaler, client, slot := scaleFixture(t)
	client.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		// Simulate an installer changing the template after Observe validated the
		// Deployment, but before it receives the scale subresource's newer RV.
		resource := appsv1.SchemeGroupVersion.WithResource("deployments")
		item, err := client.Tracker().Get(resource, slot.Namespace, slot.Name)
		if err != nil {
			return true, nil, err
		}
		deployment, ok := item.(*appsv1.Deployment)
		if !ok || deployment == nil {
			t.Fatalf("unexpected deployment fixture %T", item)
		}
		changed := deployment.DeepCopy()
		changed.Spec.Template.Spec.Containers[0].Image = "image@sha256:unapproved"
		changed.ResourceVersion = "new-rv"
		if err := client.Tracker().Update(resource, changed, slot.Namespace); err != nil {
			return true, nil, err
		}
		return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: slot.Name, UID: types.UID(slot.UID), ResourceVersion: changed.ResourceVersion}, Spec: autoscalingv1.ScaleSpec{Replicas: 1}}, nil
	})
	observation, err := scaler.Observe(context.Background(), slot)
	if !errors.Is(err, apperrors.ErrConflict) || observation.ResourceVersion != "" {
		t.Fatalf("mixed template/scale snapshot accepted: %+v %v", observation, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" || action.GetVerb() == "patch" {
			t.Fatal("mutated workload after inconsistent observation")
		}
	}
}

func TestScaleWritesOnlyNamedScaleSubresource(t *testing.T) {
	scaler, client, slot := scaleFixture(t)
	writes := 0
	client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		writes++
		if action.GetSubresource() != "scale" {
			t.Fatal("wrote Deployment body")
		}
		update, ok := action.(ktesting.UpdateAction)
		if !ok || update == nil {
			t.Fatalf("unexpected update action %T", action)
		}
		scale, ok := update.GetObject().(*autoscalingv1.Scale)
		if !ok || scale == nil {
			t.Fatalf("unexpected scale fixture %T", update.GetObject())
		}
		if scale.Spec.Replicas != 2 || scale.UID != "uid" || scale.ResourceVersion != "rv" {
			t.Fatalf("unsafe scale: %+v", scale)
		}
		return true, scale, nil
	})
	observation, err := scaler.Observe(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if err = scaler.Scale(context.Background(), slot, observation, 2); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatal(writes)
	}
	if err = scaler.Scale(context.Background(), slot, observation, 4); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatal("out-of-policy write")
	}
}

func TestObservationRejectsUIDAndTemplateDrift(t *testing.T) {
	for _, field := range []string{"uid", "template"} {
		t.Run(field, func(t *testing.T) {
			scaler, client, slot := scaleFixture(t)
			if field == "uid" {
				slot.UID = "replacement"
			} else {
				slot.TemplateDigest = "sha256:unapproved"
			}
			if _, err := scaler.Observe(context.Background(), slot); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "update" {
					t.Fatal("wrote during observation")
				}
			}
		})
	}
}
