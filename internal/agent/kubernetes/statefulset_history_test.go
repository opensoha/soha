package kubernetes

import (
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
	"testing"
)

func TestStatefulSetHistoryOwnershipAndPodCounts(t *testing.T) {
	controller := true
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "team", UID: "new"}}
	owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db", UID: sts.UID, Controller: &controller}
	revision := func(name string, uid types.UID, seq int64) *appsv1.ControllerRevision {
		ref := owner
		ref.UID = uid
		return &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", OwnerReferences: []metav1.OwnerReference{ref}}, Revision: seq, Data: runtime.RawExtension{Raw: []byte(`{"spec":{"template":{"spec":{"containers":[{"name":"db","image":"db:v2"}]}}}}`)}}
	}
	pod := func(name, rev string, uid types.UID, ready bool) *corev1.Pod {
		ref := owner
		ref.UID = uid
		state := corev1.ConditionFalse
		if ready {
			state = corev1.ConditionTrue
		}
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", Labels: map[string]string{appsv1.ControllerRevisionHashLabelKey: rev}, OwnerReferences: []metav1.OwnerReference{ref}}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: state}}}}
	}
	foreign := revision("foreign", "old", 99)
	noncontroller := revision("noncontroller", "new", 100)
	noncontroller.OwnerReferences[0].Controller = nil
	typed := fake.NewClientset(sts, revision("db-1", "new", 1), revision("db-2", "new", 2), foreign, noncontroller, pod("db-0", "db-2", "new", true), pod("db-1", "db-2", "new", false), pod("db-old", "db-2", "old", true))
	rows, err := listStatefulSetHistory(context.Background(), typed, "team", "db")
	if err != nil || len(rows) != 2 {
		t.Fatalf("history=%#v err=%v", rows, err)
	}
	if rows[0].Name != "db-2" || rows[0].Replicas != 2 || rows[0].ReadyReplicas != 1 || len(rows[0].Images) != 1 || rows[1].Replicas != 0 {
		t.Fatalf("incorrect observed history: %#v", rows)
	}
	for _, name := range []string{"foreign", "noncontroller"} {
		if _, err := statefulSetRevisionYAML(context.Background(), typed, "team", "db", name); !apierrors.IsNotFound(err) {
			t.Fatalf("foreign revision %s readable: %v", name, err)
		}
	}
	view, err := statefulSetRevisionYAML(context.Background(), typed, "team", "db", "db-2")
	if err != nil {
		t.Fatal(err)
	}
	var object appsv1.ControllerRevision
	if err := yaml.Unmarshal([]byte(view.Content), &object); err != nil {
		t.Fatal(err)
	}
	if object.Kind != "ControllerRevision" || object.Name != "db-2" || object.Revision != 2 {
		t.Fatalf("wrong revision YAML: %#v", object)
	}
	for _, action := range typed.Actions() {
		if action.GetResource().Resource == "secrets" {
			t.Fatal("history must never resolve secrets")
		}
	}
}
func TestStatefulSetHistoryPropagatesReadFailure(t *testing.T) {
	typed := fake.NewClientset(&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "team", UID: "new"}})
	denied := errors.New("revision read denied")
	typed.PrependReactor("list", "controllerrevisions", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, denied })
	if _, err := listStatefulSetHistory(context.Background(), typed, "team", "db"); !errors.Is(err, denied) {
		t.Fatalf("lost error: %v", err)
	}
}
