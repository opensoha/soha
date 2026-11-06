package runtime

import (
	cr "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	ResourceGraphRelationMounts           = "mounts"
	ResourceGraphRelationBindsTo          = "binds-to"
	ResourceGraphRelationUsesStorageClass = "uses-storage-class"
)

func graphStorageRootKind(kind string) bool {
	switch kind {
	case "PersistentVolumeClaim", "PersistentVolume", "StorageClass":
		return true
	}
	return false
}

func graphStorageClassName(o *unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(o.Object, "spec", "storageClassName")
	return name
}

func graphClaimVolumeName(claim *unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(claim.Object, "spec", "volumeName")
	return name
}

func graphVolumeClaimName(volume map[string]interface{}) string {
	return graphString(graphMap(volume, "persistentVolumeClaim"), "claimName")
}

func graphVolumeClaimRef(volume *unstructured.Unstructured) (graphNamespacedName, string) {
	ref, _, _ := graphNestedMap(volume.Object, "spec", "claimRef")
	if api := graphString(ref, "apiVersion"); api != "" && api != "v1" {
		return graphNamespacedName{}, ""
	}
	if kind := graphString(ref, "kind"); kind != "" && kind != "PersistentVolumeClaim" {
		return graphNamespacedName{}, ""
	}
	return graphNamespacedName{namespace: graphString(ref, "namespace"), name: graphString(ref, "name")}, graphString(ref, "uid")
}

func graphStorageBound(claim, volume *unstructured.Unstructured) bool {
	if claim == nil || volume == nil || claim.GetDeletionTimestamp() != nil || volume.GetDeletionTimestamp() != nil {
		return false
	}
	if graphStatus(claim) != string(corev1.ClaimBound) || graphStatus(volume) != string(corev1.VolumeBound) {
		return false
	}
	ref, uid := graphVolumeClaimRef(volume)
	return ref.namespace != "" && ref.name != "" && uid != "" && uid == string(claim.GetUID()) &&
		ref.namespace == claim.GetNamespace() && ref.name == claim.GetName() && graphClaimVolumeName(claim) == volume.GetName()
}

func (b *graphRules) storageRelations(o *unstructured.Unstructured, id string) {
	if !graphBuiltinSpec(o) {
		return
	}
	switch o.GetKind() {
	case "Pod":
		for _, raw := range graphSlice(graphPodSpec(o), "volumes") {
			if !b.active() {
				return
			}
			volume, _ := raw.(map[string]interface{})
			b.storageMountEdge(id, o.GetNamespace(), graphVolumeClaimName(volume))
		}
		return
	case "PersistentVolumeClaim":
		if o.GetNamespace() == "" {
			return
		}
		if volumeName := graphClaimVolumeName(o); volumeName != "" {
			volume := b.objects[graphIdentity("v1", "PersistentVolume", "", volumeName)]
			edge := cr.ResourceGraphEdge{SourceID: id, Relation: ResourceGraphRelationBindsTo, FieldPath: "spec.volumeName"}
			if graphStorageBound(o, volume) {
				edge.TargetID = b.node(b.ref(volume), graphStatus(volume))
				edge.ResolutionState = "resolved"
				edge.Note = "PVC and PV report Bound with matching references"
			} else {
				edge.TargetID = b.unresolvedStorageNode("PersistentVolume", "", volumeName, "")
				edge.ResolutionState = "unresolved"
				edge.Note = "PVC volume reference requires binding confirmation"
			}
			b.addEdge(edge)
		}
	case "PersistentVolume":
		if o.GetNamespace() != "" {
			return
		}
		ref, uid := graphVolumeClaimRef(o)
		if ref.namespace != "" && ref.name != "" {
			claim := b.objects[graphIdentity("v1", "PersistentVolumeClaim", ref.namespace, ref.name)]
			if !graphStorageBound(claim, o) {
				declaration := b.ref(o)
				b.addEdge(cr.ResourceGraphEdge{
					SourceID: b.unresolvedStorageNode("PersistentVolumeClaim", ref.namespace, ref.name, uid),
					TargetID: id, Relation: ResourceGraphRelationBindsTo, ResolutionState: "unresolved",
					FieldPath: "spec.claimRef", Note: "PV claim reference requires binding confirmation", SourceRef: &declaration,
				})
			}
		}
	default:
		return
	}
	b.storageReferenceEdge(id, "storage.k8s.io/v1", "StorageClass", "", graphStorageClassName(o),
		ResourceGraphRelationUsesStorageClass, "spec.storageClassName", "StorageClass reference declared in spec")
}

func (b *graphRules) storageMountEdge(source, namespace, claim string) {
	if namespace != "" {
		b.storageReferenceEdge(source, "v1", "PersistentVolumeClaim", namespace, claim,
			ResourceGraphRelationMounts, "spec.volumes", "PVC reference declared in Pod spec")
	}
}

func (b *graphRules) unresolvedStorageNode(kind, namespace, name, uid string) string {
	ref := cr.ResourceRef{ClusterID: b.cluster, APIVersion: "v1", Kind: kind, Namespace: namespace, Name: name, UID: uid, ScopeMode: topologyScope(namespace != "")}
	id := graphNodeID(ref)
	if observed := b.objects[graphIdentity(ref.APIVersion, kind, namespace, name)]; observed != nil && graphNodeID(b.ref(observed)) == id {
		// A one-sided declaration can name an observed object without binding it.
		id += ":unresolved"
	}
	return b.addNode(id, ref, "unresolved")
}

func (b *graphRules) storageReferenceEdge(source, api, kind, namespace, name, relation, fieldPath, note string) {
	if name == "" {
		return
	}
	state := "unresolved"
	if b.objects[graphIdentity(api, kind, namespace, name)] != nil {
		state = "resolved"
	}
	b.addEdge(cr.ResourceGraphEdge{
		SourceID: source, TargetID: b.target(api, kind, namespace, name, ""), Relation: relation,
		ResolutionState: state, FieldPath: fieldPath, Note: note,
	})
}
