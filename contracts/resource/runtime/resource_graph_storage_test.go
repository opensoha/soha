package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func storageGraphFixture() (*corev1.Pod, *corev1.PersistentVolumeClaim, *corev1.PersistentVolume, *storagev1.StorageClass) {
	className := "fast"
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "team", UID: "claim-uid"},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "disk", StorageClassName: &className},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	volume := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "disk", UID: "volume-uid"},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName: className,
			ClaimRef:         &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "team", Name: "data", UID: claim.UID},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team", UID: "pod-uid"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{
			{Name: "first", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name}}},
			{Name: "duplicate", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name, ReadOnly: true}}},
		}},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	class := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: className, UID: "class-uid"}}
	return pod, claim, volume, class
}

func collectStorageGraph(t *testing.T, client *fake.Clientset, namespace, kind, name string) cr.ResourceGraph {
	t.Helper()
	graph, err := BuildKubernetesResourceGraph(context.Background(), client, cr.ResourceRef{ClusterID: "cluster", Namespace: namespace, Kind: kind, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func assertStorageGraphActions(t *testing.T, client *fake.Clientset, want []string) {
	t.Helper()
	got := []string{}
	for _, action := range client.Actions() {
		record := action.GetVerb() + " " + action.GetResource().Resource + "/" + action.GetNamespace()
		switch action.GetVerb() {
		case "get":
			get, ok := action.(ktesting.GetAction)
			if !ok {
				t.Fatalf("get action has no resource name: %+v", action)
			}
			record += "/" + get.GetName()
		case "list":
			list, ok := action.(interface{ GetListOptions() metav1.ListOptions })
			if !ok {
				t.Fatalf("list action has no list options: %+v", action)
			}
			options := list.GetListOptions()
			if options.Limit != ResourceGraphSourceLimit || options.Continue != "" {
				t.Fatalf("unbounded source options for %s: %+v", record, options)
			}
		}
		got = append(got, record)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func storageGraphNodes(graph cr.ResourceGraph) map[string]cr.ResourceGraphNode {
	nodes := map[string]cr.ResourceGraphNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	return nodes
}

func TestResourceGraphStorageCollectorRootsAndScopes(t *testing.T) {
	for _, tc := range []struct {
		kind, name, namespace string
		calls                 []string
	}{
		{"persistentvolumeclaim", "data", "team", []string{"get persistentvolumeclaims/team/data", "get persistentvolumes//disk", "get storageclasses//fast", "list pods/team"}},
		{"PersistentVolume", "disk", "", []string{"get persistentvolumes//disk", "get persistentvolumeclaims/team/data", "get storageclasses//fast", "list pods/team"}},
		{"storageclass", "fast", "", []string{"get storageclasses//fast", "list persistentvolumes/", "list persistentvolumeclaims/", "list pods/"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			pod, claim, volume, class := storageGraphFixture()
			pod.Spec.NodeName = "unrelated-node"
			pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "custom.example/v1", Kind: "UnrelatedOwner", Name: "unrelated-owner", UID: "owner-uid"}}
			claim.OwnerReferences = pod.OwnerReferences
			foreignPod := pod.DeepCopy()
			foreignPod.Name, foreignPod.Namespace, foreignPod.UID = "foreign-pod", "other", "foreign-pod-uid"
			foreignClaim := claim.DeepCopy()
			foreignClaim.Namespace, foreignClaim.UID, foreignClaim.Spec.StorageClassName = "other", "foreign-claim-uid", nil
			client := fake.NewClientset(pod, claim, volume, class, foreignPod, foreignClaim)
			graph := collectStorageGraph(t, client, tc.namespace, tc.kind, tc.name)
			assertStorageGraphActions(t, client, tc.calls)
			if len(graph.Nodes) != 4 || len(graph.Warnings) != 0 || graphTestHas(graph, "foreign-pod") {
				t.Fatalf("unexpected graph: %+v", graph)
			}
			nodes := storageGraphNodes(graph)
			if nodes[graph.RootID].Resource.Name != tc.name || nodes[graph.RootID].Resource.Namespace != tc.namespace {
				t.Fatalf("wrong root: %+v", nodes[graph.RootID])
			}
			matches := map[string]int{}
			for _, edge := range graph.Edges {
				if edge.ResolutionState != "resolved" {
					t.Fatalf("unexpected resolution: %+v", edge)
				}
				matches[edge.Relation+"/"+nodes[edge.SourceID].Resource.Name+"/"+nodes[edge.TargetID].Resource.Name]++
				if edge.Relation == ResourceGraphRelationMounts && (edge.Note != "PVC reference declared in Pod spec" || nodes[edge.SourceID].Status != "Pending") {
					t.Fatalf("spec reference became mount success: %+v", edge)
				}
			}
			for _, expected := range []string{"mounts/app/data", "binds-to/data/disk", "uses-storage-class/disk/fast"} {
				if matches[expected] != 1 {
					t.Errorf("%s count = %d, want 1: %v", expected, matches[expected], matches)
				}
			}
			if !strings.EqualFold(tc.kind, "PersistentVolume") && matches["uses-storage-class/data/fast"] != 1 {
				t.Errorf("missing declared claim class: %v", matches)
			}
		})
	}
}

func TestResourceGraphStorageClassFollowsConfirmedVolumeToClaimWithoutClass(t *testing.T) {
	pod, claim, volume, class := storageGraphFixture()
	claim.Spec.StorageClassName = nil
	client := fake.NewClientset(pod, claim, volume, class)
	graph := collectStorageGraph(t, client, "", "StorageClass", "fast")
	assertStorageGraphActions(t, client, []string{"get storageclasses//fast", "list persistentvolumes/", "list persistentvolumeclaims/", "list pods/"})
	if !graphTestEdge(graph, "mounts", "app", "data") || !graphTestEdge(graph, "binds-to", "data", "disk") || graphTestEdge(graph, "uses-storage-class", "data", "fast") {
		t.Fatalf("class root lost the binding chain or invented a claim class: %+v", graph)
	}
}

func TestResourceGraphStorageCollectorStaleAndOneSidedBindings(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*corev1.PersistentVolumeClaim, *corev1.PersistentVolume)
	}{
		{"recreated claim", func(claim *corev1.PersistentVolumeClaim, _ *corev1.PersistentVolume) { claim.UID = "new-uid" }},
		{"another namespace", func(_ *corev1.PersistentVolumeClaim, volume *corev1.PersistentVolume) {
			volume.Spec.ClaimRef.Namespace = "other"
		}},
		{"another name", func(_ *corev1.PersistentVolumeClaim, volume *corev1.PersistentVolume) {
			volume.Spec.ClaimRef.Name = "other"
		}},
		{"no UID", func(_ *corev1.PersistentVolumeClaim, volume *corev1.PersistentVolume) { volume.Spec.ClaimRef.UID = "" }},
		{"no reciprocal volume", func(claim *corev1.PersistentVolumeClaim, _ *corev1.PersistentVolume) { claim.Spec.VolumeName = "" }},
		{"another reciprocal volume", func(claim *corev1.PersistentVolumeClaim, _ *corev1.PersistentVolume) { claim.Spec.VolumeName = "other" }},
		{"terminating claim", func(claim *corev1.PersistentVolumeClaim, _ *corev1.PersistentVolume) {
			now := metav1.Now()
			claim.DeletionTimestamp = &now
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod, claim, volume, class := storageGraphFixture()
			tc.edit(claim, volume)
			client := fake.NewClientset(pod, claim, volume, class)
			graph := collectStorageGraph(t, client, "", "PersistentVolume", "disk")
			for _, action := range client.Actions() {
				if action.GetResource().Resource == "pods" {
					t.Fatalf("unconfirmed claim triggered a Pod read: %+v", action)
				}
			}
			nodes := storageGraphNodes(graph)
			count := 0
			for _, edge := range graph.Edges {
				if edge.Relation != ResourceGraphRelationBindsTo {
					continue
				}
				count++
				source := nodes[edge.SourceID]
				ref := volume.Spec.ClaimRef
				if edge.TargetID != graph.RootID || edge.ResolutionState != "unresolved" || source.Status != "unresolved" || source.Resource.UID != string(ref.UID) || source.Resource.Namespace != ref.Namespace || source.Resource.Name != ref.Name {
					t.Fatalf("binding attached an observed claim or wrong declaration: %+v, %+v", edge, source)
				}
				if edge.FieldPath != "spec.claimRef" || edge.SourceRef == nil || *edge.SourceRef != nodes[graph.RootID].Resource {
					t.Fatalf("reversed edge does not identify the PV declaration: %+v", edge)
				}
			}
			if count != 1 || graphTestHas(graph, pod.Name) {
				t.Fatalf("unexpected graph: %+v", graph)
			}
		})
	}
	t.Run("claim side excludes an observed mismatched volume", func(t *testing.T) {
		pod, claim, volume, class := storageGraphFixture()
		volume.Spec.ClaimRef.UID = "old-claim-uid"
		client := fake.NewClientset(pod, claim, volume, class)
		graph := collectStorageGraph(t, client, "team", "PersistentVolumeClaim", "data")
		nodes := storageGraphNodes(graph)
		for _, edge := range graph.Edges {
			if edge.Relation != ResourceGraphRelationBindsTo {
				continue
			}
			target := nodes[edge.TargetID]
			if edge.SourceID != graph.RootID || edge.ResolutionState != "unresolved" || target.Status != "unresolved" || target.Resource.UID != "" || target.Resource.Name != volume.Name {
				t.Fatalf("binding attached an unrelated observed volume: %+v, %+v", edge, target)
			}
			return
		}
		t.Fatal("PVC volume declaration missing")
	})
}

func TestResourceGraphStorageBindingRequiresBothBoundPhases(t *testing.T) {
	for _, tc := range []struct {
		name        string
		claimPhase  corev1.PersistentVolumeClaimPhase
		volumePhase corev1.PersistentVolumePhase
	}{
		{"pending claim", corev1.ClaimPending, corev1.VolumeBound},
		{"lost claim", corev1.ClaimLost, corev1.VolumeBound},
		{"missing claim phase", "", corev1.VolumeBound},
		{"released volume", corev1.ClaimBound, corev1.VolumeReleased},
		{"failed volume", corev1.ClaimBound, corev1.VolumeFailed},
		{"missing volume phase", corev1.ClaimBound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod, claim, volume, class := storageGraphFixture()
			claim.Status.Phase = tc.claimPhase
			volume.Status.Phase = tc.volumePhase
			for _, root := range []struct {
				namespace, kind, name, phase string
			}{
				{claim.Namespace, "PersistentVolumeClaim", claim.Name, string(tc.claimPhase)},
				{"", "PersistentVolume", volume.Name, string(tc.volumePhase)},
			} {
				graph := collectStorageGraph(t, fake.NewClientset(pod, claim, volume, class), root.namespace, root.kind, root.name)
				if node := storageGraphNodes(graph)[graph.RootID]; node.Status != root.phase {
					t.Errorf("%s phase = %q, want observed phase %q", root.kind, node.Status, root.phase)
				}
				if edges := networkEdges(graph, ResourceGraphRelationBindsTo); len(edges) != 1 || edges[0].ResolutionState != "unresolved" {
					t.Errorf("%s needs one unresolved declaration, got %+v", root.kind, edges)
				}
			}
		})
	}
}

func TestResourceGraphStorageCollectorUnboundAndUnavailableSources(t *testing.T) {
	t.Run("explicit empty or nil class stays absent", func(t *testing.T) {
		empty := ""
		for _, className := range []*string{nil, &empty} {
			pod, claim, _, class := storageGraphFixture()
			claim.Spec.VolumeName, claim.Spec.StorageClassName, claim.Status.Phase = "", className, corev1.ClaimPending
			class.Annotations = map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}
			client := fake.NewClientset(pod, claim, class)
			graph := collectStorageGraph(t, client, "team", "PersistentVolumeClaim", "data")
			assertStorageGraphActions(t, client, []string{"get persistentvolumeclaims/team/data", "list pods/team"})
			if storageGraphNodes(graph)[graph.RootID].Status != "Pending" || len(graph.Warnings) != 0 || len(graph.Edges) != 1 || graph.Edges[0].Relation != ResourceGraphRelationMounts {
				t.Fatalf("unbound claim state lost: %+v", graph)
			}
		}
	})
	t.Run("missing volume and forbidden class", func(t *testing.T) {
		pod, claim, _, _ := storageGraphFixture()
		client := fake.NewClientset(pod, claim)
		client.PrependReactor("get", "storageclasses", func(ktesting.Action) (bool, kruntime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "storage.k8s.io", Resource: "storageclasses"}, "private-name", errors.New("private error"))
		})
		graph := collectStorageGraph(t, client, "team", "PersistentVolumeClaim", "data")
		want := []string{"PersistentVolume reference source NotFound", "StorageClass reference source Forbidden"}
		if !slices.Equal(graph.Warnings, want) {
			t.Fatalf("warnings = %v, want %v", graph.Warnings, want)
		}
		nodes := storageGraphNodes(graph)
		for _, edge := range graph.Edges {
			if edge.Relation != ResourceGraphRelationMounts && (edge.ResolutionState != "unresolved" || nodes[edge.TargetID].Status != "unresolved" || nodes[edge.TargetID].Resource.UID != "") {
				t.Fatalf("unavailable target resolved: %+v", edge)
			}
		}
	})
	t.Run("truncated class claims do not trigger an unrelated Pod scan", func(t *testing.T) {
		pod, _, volume, class := storageGraphFixture()
		client := fake.NewClientset(pod, volume, class)
		client.PrependReactor("list", "persistentvolumeclaims", func(ktesting.Action) (bool, kruntime.Object, error) {
			return true, &corev1.PersistentVolumeClaimList{ListMeta: metav1.ListMeta{Continue: "second-page"}}, nil
		})
		graph := collectStorageGraph(t, client, "", "StorageClass", "fast")
		assertStorageGraphActions(t, client, []string{"get storageclasses//fast", "list persistentvolumes/", "list persistentvolumeclaims/"})
		if !slices.Equal(graph.Warnings, []string{"PersistentVolumeClaim source truncated at 500 objects"}) || !graphTestEdge(graph, "binds-to", "data", "disk") {
			t.Fatalf("truncated source or declared claim missing: %+v", graph)
		}
		for _, node := range graph.Nodes {
			if node.Resource.Kind == "PersistentVolumeClaim" && (node.Resource.UID != "claim-uid" || node.Status != "unresolved") {
				t.Fatalf("unobserved claim resolved: %+v", node)
			}
		}
	})
	t.Run("canceled sources fail the request", func(t *testing.T) {
		_, claim, volume, class := storageGraphFixture()
		client := fake.NewClientset(claim, volume, class)
		client.PrependReactor("list", "pods", func(ktesting.Action) (bool, kruntime.Object, error) { return true, nil, context.DeadlineExceeded })
		_, err := BuildKubernetesResourceGraph(context.Background(), client, cr.ResourceRef{ClusterID: "cluster", Namespace: "team", Kind: "PersistentVolumeClaim", Name: "data"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation did not stop graph: %v", err)
		}
	})
}

func TestResourceGraphStorageCollectorRejectsInvalidRoots(t *testing.T) {
	for _, root := range []cr.ResourceRef{
		{Kind: "PersistentVolumeClaim", Name: "data"},
		{Kind: "PersistentVolumeClaim", Name: "data", Namespace: "team", ScopeMode: cr.ResourceScopeModeCluster},
		{Kind: "PersistentVolume", Name: "disk", Namespace: "team"},
		{Kind: "StorageClass", Name: "fast", Namespace: "team"},
		{Kind: "StorageClass", Name: "fast", ScopeMode: cr.ResourceScopeModeNamespace},
		{Kind: "StorageClass", Name: "fast", APIVersion: "v1"},
	} {
		root.ClusterID = "cluster"
		client := fake.NewClientset()
		_, err := BuildKubernetesResourceGraph(context.Background(), client, root)
		if !errors.Is(err, ErrInvalidResourceGraphRoot) || len(client.Actions()) != 0 {
			t.Errorf("invalid root %+v triggered provider actions: %v, %v", root, err, client.Actions())
		}
	}
	_, claim, volume, class := storageGraphFixture()
	for _, mutate := range []func(*corev1.PersistentVolume){
		func(item *corev1.PersistentVolume) { item.Name = "different" },
		func(item *corev1.PersistentVolume) { item.Namespace = "team" },
		func(item *corev1.PersistentVolume) {
			item.APIVersion, item.Kind = "custom.example/v1", "PersistentVolume"
		},
	} {
		mismatched := volume.DeepCopy()
		mutate(mismatched)
		client := fake.NewClientset(claim, volume, class)
		client.PrependReactor("get", "persistentvolumes", func(ktesting.Action) (bool, kruntime.Object, error) { return true, mismatched, nil })
		_, err := BuildKubernetesResourceGraph(context.Background(), client, cr.ResourceRef{ClusterID: "cluster", Kind: "PersistentVolume", Name: "disk"})
		if !errors.Is(err, ErrInvalidResourceGraphRoot) {
			t.Errorf("changed named root was accepted: %v", err)
		}
		assertStorageGraphActions(t, client, []string{"get persistentvolumes//disk"})
	}
}

func TestResourceGraphStorageProjectionAndExistingPodRoot(t *testing.T) {
	pod, claim, volume, class := storageGraphFixture()
	volume.Labels = map[string]string{"private": "private-label"}
	volume.Spec.CSI = &corev1.CSIPersistentVolumeSource{
		Driver: "private-driver", VolumeHandle: "private-handle", VolumeAttributes: map[string]string{"token": "private-token"},
		NodePublishSecretRef: &corev1.SecretReference{Name: "private-secret", Namespace: "private-namespace"},
	}
	volume.Spec.ClaimRef.FieldPath = "private-field"
	class.Provisioner = "private-provisioner"
	class.Parameters = map[string]string{"key": "private-value"}
	for _, tc := range []struct {
		obj  kruntime.Object
		kind string
	}{
		{volume, "PersistentVolume"}, {class, "StorageClass"},
	} {
		projection, err := graphObjectProjection(tc.obj, graphBuiltinAPIs[tc.kind], tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		content, err := json.Marshal(projection)
		if err != nil || strings.Contains(string(content), "private-") {
			t.Fatalf("storage projection retained provider payload: %s, %v", content, err)
		}
	}
	client := fake.NewClientset(pod, claim, volume, class)
	graph := collectStorageGraph(t, client, "team", "Pod", "app")
	for _, action := range client.Actions() {
		if slices.Contains([]string{"persistentvolumes", "storageclasses", "secrets"}, action.GetResource().Resource) {
			t.Fatalf("existing Pod root issued new storage or Secret read: %+v", action)
		}
	}
	if !reflect.DeepEqual(graph.Edges, []cr.ResourceGraphEdge{{
		ID: "mounts:v1:Pod:team/app:pod-uid:v1:PersistentVolumeClaim:team/data:claim-uid", SourceID: graph.RootID, TargetID: "v1:PersistentVolumeClaim:team/data:claim-uid", Relation: "mounts",
	}}) {
		t.Fatalf("existing Pod PVC edge changed: %+v", graph.Edges)
	}
}
