package resourcebackend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

func deliveryTestObject(name, uid string, owners ...metav1.OwnerReference) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: "apps", Name: name, UID: types.UID(uid), Labels: map[string]string{"app": "same"}, OwnerReferences: owners}
}

func deliveryTestOwner(version, kind, name, uid string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{APIVersion: version, Kind: kind, Name: name, UID: types.UID(uid), Controller: &controller}
}

func TestDeliveryDeploymentPodsUseSelectorAndBothOwnerUIDs(t *testing.T) {
	const api = "apps/v1"
	root := &appsv1.Deployment{ObjectMeta: deliveryTestObject("api", "current"), Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"same"}}}}}}
	owned := &appsv1.ReplicaSet{ObjectMeta: deliveryTestObject("rs", "rs-current", deliveryTestOwner(api, "Deployment", "api", "current"))}
	foreign := &appsv1.ReplicaSet{ObjectMeta: deliveryTestObject("foreign", "rs-foreign", deliveryTestOwner(api, "Deployment", "other", "other-uid"))}
	makePod := func(name, parent, ownerUID string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: deliveryTestObject(name, name+"-uid", deliveryTestOwner(api, "ReplicaSet", parent, ownerUID))}
	}
	uncontrolled := makePod("no-controller", "rs", "rs-current")
	uncontrolled.OwnerReferences[0].Controller = nil
	typed := fake.NewSimpleClientset(root, owned, foreign, makePod("good", "rs", "rs-current"), makePod("wrong-controller", "foreign", "rs-foreign"), makePod("old-replicaset", "rs", "rs-old"), makePod("broken", "missing", "missing-uid"), uncontrolled)
	ref := dr.ResourceRef{APIVersion: api, ClusterID: "demo", Kind: "Deployment", Name: "api", Namespace: "apps", UID: "current"}
	pods, err := listDeliveryControllerPods(t.Context(), typed, ref)
	if len(pods) != 1 || pods[0].UID != "good-uid" || !errors.Is(err, dr.ErrDeliveryPodOwnershipUnverified) {
		t.Fatalf("projection=%+v, err=%v", pods, err)
	}
	for _, action := range typed.Actions() {
		if !action.Matches("list", "pods") {
			continue
		}
		list, ok := action.(clienttesting.ListAction)
		if !ok || list.GetListRestrictions().Labels.Empty() {
			t.Fatal("pod list used an unbounded namespace scan")
		}
	}
	typed.ClearActions()
	ref.UID = "old"
	pods, err = listDeliveryControllerPods(t.Context(), typed, ref)
	if len(pods) != 0 || !errors.Is(err, apperrors.ErrConflict) || len(typed.Actions()) != 1 {
		t.Fatalf("same-name replacement projection=%+v, err=%v, calls=%v", pods, err, typed.Actions())
	}
}

func TestDeliveryCronJobPodsUseOnlyControlledJob(t *testing.T) {
	const api = "batch/v1"
	cron := &batchv1.CronJob{ObjectMeta: deliveryTestObject("schedule", "cron-uid")}
	job := &batchv1.Job{ObjectMeta: deliveryTestObject("run", "job-uid", deliveryTestOwner(api, "CronJob", "schedule", "cron-uid"))}
	other := &batchv1.Job{ObjectMeta: deliveryTestObject("other", "other-uid", deliveryTestOwner(api, "CronJob", "schedule", "stale-cron-uid"))}
	pod := &corev1.Pod{ObjectMeta: deliveryTestObject("batch", "pod-uid", deliveryTestOwner(api, "Job", "run", "job-uid"))}
	pod.Labels["batch.kubernetes.io/job-name"] = "run"
	unowned := pod.DeepCopy()
	unowned.Name, unowned.UID = "unowned", "unowned-uid"
	unowned.OwnerReferences[0].UID = "other-uid"
	typed := fake.NewSimpleClientset(cron, job, other, pod, unowned)
	ref := dr.ResourceRef{APIVersion: api, Kind: "CronJob", Name: "schedule", Namespace: "apps", UID: "cron-uid"}
	pods, err := listDeliveryControllerPods(t.Context(), typed, ref)
	if len(pods) != 1 || pods[0].UID != "pod-uid" || err != nil {
		t.Fatalf("cron projection=%+v, err=%v", pods, err)
	}
}

func TestDeliveryPodListDedupesOnlySameRequestClientNamespaceAndSelector(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		if req.URL.Query().Get("labelSelector") == "" {
			t.Error("expected selector pushdown")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList","apiVersion":"v1","items":[]}`))
	}))
	defer server.Close()
	client := kubernetes.NewForConfigOrDie(&rest.Config{Host: server.URL})
	otherClient := kubernetes.NewForConfigOrDie(&rest.Config{Host: server.URL})
	backend := &Direct{}
	ctx := backend.WithDeliveryPodLists(t.Context())
	for _, test := range []struct {
		ctx       context.Context
		client    *kubernetes.Clientset
		namespace string
		selector  string
		readCount int32
	}{
		{ctx, client, "team", "app=api", 1},
		{ctx, client, "team", "app=api", 1},
		{ctx, client, "team", "app=web", 2},
		{ctx, client, "other-team", "app=api", 3},
		{ctx, otherClient, "team", "app=api", 4},
		{backend.WithDeliveryPodLists(t.Context()), client, "team", "app=api", 5},
	} {
		if _, err := readDeliverySelectedPods(test.ctx, test.client, test.namespace, test.selector); err != nil || requests.Load() != test.readCount {
			t.Fatalf("pod list=%s/%s requests=%d want=%d err=%v", test.namespace, test.selector, requests.Load(), test.readCount, err)
		}
	}
}

func TestDecodeDeliveryPodStatusVerifiesUIDAndKeepsLastExit(t *testing.T) {
	yaml := `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"child","namespace":"apps","uid":"current",
	"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"db","uid":"owner-current","controller":true}]},
	"spec":{"containers":[{"name":"api","env":[{"name":"SECRET","value":"hidden"}]}]},
	"status":{"containerStatuses":[{"name":"api","restartCount":2,"state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"reason":"OOMKilled","exitCode":137}}}]}}`
	ref := dr.ResourceRef{Name: "child", Namespace: "apps", UID: "current"}
	owner := dr.ResourceRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db", UID: "owner-current"}
	pod, err := (&Direct{}).DecodeDeliveryPodStatus(yaml, ref, owner)
	if err != nil || len(pod.ContainerStatuses) != 1 || pod.ContainerStatuses[0].LastTerminationReason != "OOMKilled" || pod.ContainerStatuses[0].LastTerminationExitCode == nil || *pod.ContainerStatuses[0].LastTerminationExitCode != 137 {
		t.Fatalf("pod status=%+v, err=%v", pod, err)
	}
	wrongPod := ref
	wrongPod.UID = "previous"
	if pod, err := (&Direct{}).DecodeDeliveryPodStatus(yaml, wrongPod, owner); pod.UID != "" || !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("replaced status=%+v, err=%v", pod, err)
	}
	owner.UID = "owner-previous"
	if pod, err := (&Direct{}).DecodeDeliveryPodStatus(yaml, ref, owner); pod.UID != "" || !errors.Is(err, dr.ErrDeliveryPodOwnershipUnverified) {
		t.Fatalf("controller handoff without replacement exposed status: %+v, err=%v", pod, err)
	}
}
