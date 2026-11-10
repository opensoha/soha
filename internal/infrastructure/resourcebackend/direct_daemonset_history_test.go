package resourcebackend

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

func TestDaemonSetHistoryOwnershipAndPodCounts(t *testing.T) {
	controller := true
	sts := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "team", UID: "new"}}
	owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "db", UID: sts.UID, Controller: &controller}
	revision := func(name string, uid types.UID, seq int64) *appsv1.ControllerRevision {
		ref := owner
		ref.UID = uid
		return &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", Labels: map[string]string{appsv1.DefaultDaemonSetUniqueLabelKey: "hash-" + name}, OwnerReferences: []metav1.OwnerReference{ref}}, Revision: seq, Data: runtime.RawExtension{Raw: []byte(`{"spec":{"template":{"spec":{"containers":[{"name":"db","image":"db:v2"}]}}}}`)}}
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
	typed := fake.NewClientset(sts, revision("db-1", "new", 1), revision("db-2", "new", 2), foreign, noncontroller, pod("db-0", "hash-db-2", "new", true), pod("db-1", "hash-db-2", "new", false), pod("db-old", "hash-db-2", "old", true))
	rows, err := listDaemonSetHistory(context.Background(), typed, "team", "db")
	if err != nil || len(rows) != 2 {
		t.Fatalf("history=%#v err=%v", rows, err)
	}
	if rows[0].Name != "db-2" || rows[0].Replicas != 2 || rows[0].ReadyReplicas != 1 || len(rows[0].Images) != 1 || rows[1].Replicas != 0 {
		t.Fatalf("incorrect observed history: %#v", rows)
	}
	for _, name := range []string{"foreign", "noncontroller"} {
		if _, err := daemonSetRevisionYAML(context.Background(), typed, "team", "db", name); !apierrors.IsNotFound(err) {
			t.Fatalf("foreign revision %s readable: %v", name, err)
		}
	}
	view, err := daemonSetRevisionYAML(context.Background(), typed, "team", "db", "db-2")
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
func TestDaemonSetHistoryPropagatesReadFailure(t *testing.T) {
	typed := fake.NewClientset(&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "team", UID: "new"}})
	denied := errors.New("revision read denied")
	typed.PrependReactor("list", "controllerrevisions", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, denied })
	if _, err := listDaemonSetHistory(context.Background(), typed, "team", "db"); !errors.Is(err, denied) {
		t.Fatalf("lost error: %v", err)
	}
}

func TestDaemonSetNativeTemplateAndMissingHash(t *testing.T) {
	controller := true
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "team", UID: "uid"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "agent"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "agent", Image: "agent:v2"}}}}}}
	patch, err := daemonSetTemplatePatch(ds.Spec.Template)
	if err != nil {
		t.Fatal(err)
	}
	owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "DaemonSet", Name: ds.Name, UID: ds.UID, Controller: &controller}
	revision := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "node-agent-abc", Namespace: "team", OwnerReferences: []metav1.OwnerReference{owner}}, Revision: 7, Data: runtime.RawExtension{Raw: patch}}
	mismatch := revision.DeepCopy()
	mismatch.Name = "other"
	mismatch.Revision = 8
	mismatch.Data.Raw = []byte(`{"spec":{"template":{"spec":{"containers":[{"name":"agent","image":"agent:v1"}]}}}}`)
	wrongKind := revision.DeepCopy()
	wrongKind.Name = "wrong-kind"
	wrongKind.OwnerReferences[0].Kind = "StatefulSet"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "agent-pod", Namespace: "team", OwnerReferences: []metav1.OwnerReference{owner}, Labels: map[string]string{appsv1.ControllerRevisionHashLabelKey: "node-agent-abc"}}}
	typed := fake.NewClientset(ds, revision, mismatch, wrongKind, pod)
	rows, err := listDaemonSetHistory(t.Context(), typed, "team", ds.Name)
	if err != nil || len(rows) != 2 {
		t.Fatalf("%#v %v", rows, err)
	}
	if rows[0].MatchesCurrentTemplate == nil || *rows[0].MatchesCurrentTemplate || rows[1].MatchesCurrentTemplate == nil || !*rows[1].MatchesCurrentTemplate || rows[1].PodRevisionHash != "" || rows[1].Replicas != 0 {
		t.Fatalf("must preserve native match and unknown hash: %#v", rows)
	}
	if _, err := daemonSetRevisionYAML(t.Context(), typed, "team", ds.Name, "wrong-kind"); !apierrors.IsNotFound(err) {
		t.Fatalf("cross-kind revision readable: %v", err)
	}
	for _, action := range typed.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatalf("history mutated cluster: %v", action)
		}
	}
}

func TestDaemonSetDetailRejectsNonControllerPods(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name       string
		uid        types.UID
		kind, api  string
		controller *bool
		want       bool
	}{
		{"valid", "uid", "DaemonSet", "apps/v1", &yes, true}, {"old", "old", "DaemonSet", "apps/v1", &yes, false}, {"non-controller", "uid", "DaemonSet", "apps/v1", &no, false}, {"unknown-controller", "uid", "DaemonSet", "apps/v1", nil, false}, {"foreign-kind", "uid", "StatefulSet", "apps/v1", &yes, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := daemonSetControlsPod([]metav1.OwnerReference{{UID: test.uid, Kind: test.kind, APIVersion: test.api, Controller: test.controller}}, "uid"); got != test.want {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}
