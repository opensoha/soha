package resource

import (
	"fmt"
	cr "github.com/opensoha/soha-contracts/resource"
	"testing"
)

func relationRef(kind, name, uid string) cr.ResourceRef {
	return cr.ResourceRef{ClusterID: "cluster", APIVersion: "example.test/v1", Kind: kind, Namespace: "team", Name: name, UID: uid}
}
func relationChild(ref, parent cr.ResourceRef) cr.RelationObject {
	return cr.RelationObject{Resource: ref, Owners: []cr.RelationOwner{{APIVersion: parent.APIVersion, Kind: parent.Kind, Name: parent.Name, UID: parent.UID}}}
}

func TestGenericRelationsUseOneEngineForSamplesAndUnknownKind(t *testing.T) {
	for _, kind := range []string{"Elasticsearch", "FlinkDeployment", "RabbitmqCluster", "UnknownWidget"} {
		t.Run(kind, func(t *testing.T) {
			root := relationRef(kind, "root", "root-uid")
			middle := relationRef("UnregisteredChild", "middle", "middle-uid")
			pod := relationRef("Pod", "pod", "pod-uid")
			decoy := relationChild(relationRef("Pod", "decoy", "decoy-uid"), root)
			decoy.Owners[0].UID = "old-root-uid"
			snapshot := cr.RelationSnapshot{Sources: []cr.RelationSourceResult{{Coverage: cr.RelationCoverage{QueryID: "owner.0", State: "available"}, Objects: []cr.RelationObject{relationChild(middle, root), relationChild(pod, middle), decoy}}}}
			view := BuildResourceRelations(root, cr.RelationConfig{}, snapshot)
			if len(view.Nodes) != 3 || len(view.Edges) != 2 || view.Partial {
				t.Fatalf("invalid generic ownership: %+v", view)
			}
			if view.Nodes[2].Resource.UID != "pod-uid" || view.Nodes[2].Depth != 2 {
				t.Fatal("CR-to-CR chain not traversed")
			}
		})
	}
}

func TestDeclaredSharedReferenceDoesNotClaimRuntimeAndManualPinsUID(t *testing.T) {
	root := relationRef("SessionJob", "job", "job-uid")
	shared := relationRef("SessionCluster", "shared", "shared-uid")
	pod := relationRef("Pod", "other-job", "shared-pod")
	config := cr.RelationConfig{Rules: []cr.RelationRule{{ID: "shared", Type: "reference", Name: shared.Name}}}
	snapshot := cr.RelationSnapshot{Sources: []cr.RelationSourceResult{{Coverage: cr.RelationCoverage{QueryID: "owner.0", State: "available"}, Objects: []cr.RelationObject{relationChild(pod, shared)}}, {Coverage: cr.RelationCoverage{QueryID: "rule.shared", State: "available"}, Objects: []cr.RelationObject{{Resource: shared}}}}}
	view := BuildResourceRelations(root, config, snapshot)
	if len(view.Nodes) != 2 || view.Nodes[1].Role != "reference" || view.Edges[0].Type != "references" {
		t.Fatalf("shared runtime was claimed: %+v", view)
	}
	config.Rules[0].Type = "manual"
	config.Rules[0].UID = "old-shared"
	view = BuildResourceRelations(root, config, snapshot)
	if !view.Partial || len(view.Nodes) != 1 {
		t.Fatal("manual same-name replacement accepted")
	}
	config.Rules[0].UID = shared.UID
	view = BuildResourceRelations(root, config, snapshot)
	if len(view.Nodes) != 3 || view.Nodes[2].Role != "manual" {
		t.Fatal("manual association did not follow verified children")
	}
	config.Rules[0].Type = "selector"
	config.Rules[0].UID = ""
	view = BuildResourceRelations(root, config, snapshot)
	if len(view.Nodes) != 2 || view.Nodes[1].Role != "candidate" {
		t.Fatal("selector candidates claimed descendants")
	}
}

func TestRelationsBoundCyclesAndNodesAndRejectCrossNamespaceOwnership(t *testing.T) {
	root := relationRef("Widget", "root", "root")
	a := relationRef("Child", "a", "a")
	b := relationRef("Child", "b", "b")
	ca := relationChild(a, root)
	ca.Owners = append(ca.Owners, cr.RelationOwner{APIVersion: b.APIVersion, Kind: b.Kind, Name: b.Name, UID: b.UID})
	foreign := relationChild(relationRef("Pod", "foreign", "foreign"), root)
	foreign.Resource.Namespace = "elsewhere"
	snapshot := cr.RelationSnapshot{Sources: []cr.RelationSourceResult{{Coverage: cr.RelationCoverage{State: "available"}, Objects: []cr.RelationObject{ca, relationChild(b, a), foreign}}}}
	view := BuildResourceRelations(root, cr.RelationConfig{}, snapshot)
	if !view.Partial || len(view.Nodes) != 3 || len(view.Edges) != 2 {
		t.Fatalf("cycle or namespace failure: %+v", view)
	}
	snapshot.Sources[0].Objects = nil
	for i := 0; i < 600; i++ {
		snapshot.Sources[0].Objects = append(snapshot.Sources[0].Objects, relationChild(relationRef("Pod", fmt.Sprintf("pod-%d", i), fmt.Sprintf("uid-%03d", i)), root))
	}
	view = BuildResourceRelations(root, cr.RelationConfig{}, snapshot)
	if !view.Partial || len(view.Nodes) != cr.RelationMaxNodes {
		t.Fatal("node budget not enforced")
	}
}

func TestConflictingRelationIdentitiesNeverSeedOwnership(t *testing.T) {
	root := relationRef("Widget", "root", "root")
	child := relationChild(relationRef("Child", "child", "shared-uid"), root)
	conflict := child
	conflict.Resource.Name = "different"
	pod := relationChild(relationRef("Pod", "pod", "pod"), child.Resource)
	snapshot := cr.RelationSnapshot{Sources: []cr.RelationSourceResult{
		{Coverage: cr.RelationCoverage{QueryID: "owner.0", State: "available"}, Objects: []cr.RelationObject{child, pod}},
		{Coverage: cr.RelationCoverage{QueryID: "rule.manual", State: "available"}, Objects: []cr.RelationObject{conflict}},
	}}
	config := cr.RelationConfig{Rules: []cr.RelationRule{{ID: "manual", Type: "manual", UID: child.Resource.UID}}}
	view := BuildResourceRelations(root, config, snapshot)
	if !view.Partial || len(view.Nodes) != 1 || len(view.Edges) != 0 {
		t.Fatalf("conflicting identity used: %+v", view)
	}
	conflict = child
	conflict.Resource.UID = "replacement-uid"
	snapshot.Sources[1].Objects = []cr.RelationObject{conflict}
	view = BuildResourceRelations(root, config, snapshot)
	if !view.Partial || len(view.Nodes) != 1 {
		t.Fatalf("same-name replacement mixed: %+v", view)
	}
}
