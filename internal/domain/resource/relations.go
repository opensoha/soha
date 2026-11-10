package resource

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
)

// BuildResourceRelations interprets only resource identity and explicit evidence.
// Reference and selector edges never seed ownership traversal.
func BuildResourceRelations(root cr.ResourceRef, config cr.RelationConfig, snapshot cr.RelationSnapshot) cr.ResourceRelationsView {
	b := relationBuilder{view: cr.ResourceRelationsView{Root: root, Config: config, Nodes: []cr.RelationNode{}, Edges: []cr.RelationEdge{}, Coverage: []cr.RelationCoverage{}, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nodes: map[string]int{}, objects: map[string]cr.RelationObject{}, edges: map[string]bool{}}
	snapshot, conflicts := consistentRelationIdentities(root, snapshot)
	if conflicts {
		b.gap("identity_conflict")
	}
	b.addNode(root, "root", 0)
	for _, source := range snapshot.Sources {
		b.view.Coverage = append(b.view.Coverage, source.Coverage)
		if source.Coverage.State != "available" || source.Coverage.Truncated {
			b.view.Partial = true
		}
		for _, object := range source.Objects {
			if object.Resource.UID != root.UID {
				b.objects[object.Resource.UID] = object
			}
		}
	}
	queue := []string{root.UID}
	for _, rule := range config.Rules {
		queue = append(queue, b.applyRule(root, rule, snapshot)...)
	}
	uids := make([]string, 0, len(b.objects))
	for uid := range b.objects {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	for len(queue) > 0 {
		uid := queue[0]
		queue = queue[1:]
		queue = append(queue, b.expandOwned(uid, root.UID, uids)...)
	}
	return b.view
}

func (b *relationBuilder) expandOwned(uid, rootUID string, uids []string) []string {
	children := []string{}
	parent := b.view.Nodes[b.nodes[uid]]
	for _, objectUID := range uids {
		object := b.objects[objectUID]
		if !ownedRelation(parent.Resource, object) {
			continue
		}
		if b.reaches(object.Resource.UID, parent.Resource.UID) {
			b.gap("cycle")
			continue
		}
		if parent.Depth >= cr.RelationMaxDepth {
			b.gap("depth_budget")
			continue
		}
		role := "owned"
		if parent.Role == "manual" {
			role = "manual"
		}
		fresh := b.addNode(object.Resource, role, parent.Depth+1)
		if object.Resource.UID == rootUID {
			b.gap("cycle")
			continue
		}
		b.edge(parent.Resource.UID, object.Resource.UID, "owns", "")
		if fresh {
			children = append(children, object.Resource.UID)
		}
	}
	return children
}

type relationBuilder struct {
	view    cr.ResourceRelationsView
	nodes   map[string]int
	objects map[string]cr.RelationObject
	edges   map[string]bool
}

func (b *relationBuilder) addNode(ref cr.ResourceRef, role string, depth int) bool {
	if index, ok := b.nodes[ref.UID]; ok {
		previous := b.view.Nodes[index].Role
		if (previous == "reference" || previous == "candidate") && (role == "owned" || role == "manual") {
			b.view.Nodes[index].Role = role
			b.view.Nodes[index].Depth = depth
			return true
		}
		return false
	}
	if len(b.view.Nodes) >= cr.RelationMaxNodes {
		b.gap("node_budget")
		return false
	}
	b.nodes[ref.UID] = len(b.view.Nodes)
	b.view.Nodes = append(b.view.Nodes, cr.RelationNode{Resource: ref, Role: role, Depth: depth})
	return true
}

func (b *relationBuilder) edge(from, to, kind, rule string) {
	if _, ok := b.nodes[to]; !ok {
		return
	}
	if len(b.view.Edges) >= 2000 {
		b.gap("edge_budget")
		return
	}
	key := from + "/" + to + "/" + kind + "/" + rule
	if !b.edges[key] {
		b.edges[key] = true
		b.view.Edges = append(b.view.Edges, cr.RelationEdge{FromUID: from, ToUID: to, Type: kind, RuleID: rule})
	}
}

func (b *relationBuilder) reaches(from, to string) bool {
	queue := []string{from}
	seen := map[string]bool{}
	for len(queue) > 0 {
		uid := queue[0]
		queue = queue[1:]
		if uid == to {
			return true
		}
		if seen[uid] {
			continue
		}
		seen[uid] = true
		for _, edge := range b.view.Edges {
			if edge.FromUID == uid && (edge.Type == "owns" || edge.Type == "manual") {
				queue = append(queue, edge.ToUID)
			}
		}
	}
	return false
}

func (b *relationBuilder) gap(reason string) {
	b.view.Partial = true
	for _, item := range b.view.Coverage {
		if item.QueryID == "graph" && item.ReasonCode == reason {
			return
		}
	}
	b.view.Coverage = append(b.view.Coverage, cr.RelationCoverage{QueryID: "graph", State: "partial", ReasonCode: reason, Truncated: true})
}

func (b *relationBuilder) applyRule(root cr.ResourceRef, rule cr.RelationRule, snapshot cr.RelationSnapshot) []string {
	queue := []string{}
	for _, source := range snapshot.Sources {
		if source.Coverage.QueryID != "rule."+rule.ID {
			continue
		}
		for _, object := range source.Objects {
			ref := object.Resource
			if ref.UID == root.UID {
				b.gap("cycle")
				continue
			}
			if rule.UID != "" && ref.UID != rule.UID {
				b.gap("target_replaced")
				continue
			}
			role, kind := "reference", "references"
			if rule.Type == "manual" {
				role, kind = "manual", "manual"
			}
			if rule.Type == "selector" {
				role, kind = "candidate", "selector_candidate"
			}
			fresh := b.addNode(ref, role, 1)
			b.edge(root.UID, ref.UID, kind, rule.ID)
			if fresh && role == "manual" {
				queue = append(queue, ref.UID)
			}
		}
	}
	return queue
}

func ownedRelation(parent cr.ResourceRef, child cr.RelationObject) bool {
	if parent.Namespace != child.Resource.Namespace || parent.ClusterID != child.Resource.ClusterID {
		return false
	}
	for _, owner := range child.Owners {
		if owner.UID == parent.UID && owner.Name == parent.Name && owner.Kind == parent.Kind && relationAPIGroup(owner.APIVersion) == relationAPIGroup(parent.APIVersion) {
			return true
		}
	}
	return false
}

func relationAPIGroup(version string) string {
	i := strings.IndexByte(version, '/')
	if i < 0 {
		return ""
	}
	return version[:i]
}

// Conflicting observations cannot be combined into ownership evidence. Drop all
// occurrences of the affected identities, including explicit rule results.
func consistentRelationIdentities(root cr.ResourceRef, snapshot cr.RelationSnapshot) (cr.RelationSnapshot, bool) {
	seen := map[string]string{}
	names := map[string]string{}
	bad := map[string]bool{}
	rootKey := relationIdentityKey(root)
	for _, source := range snapshot.Sources {
		for _, object := range source.Objects {
			ref := object.Resource
			key := relationIdentityKey(ref)
			owners, _ := json.Marshal(object.Owners)
			evidence := key + string(owners)
			if previous, exists := seen[ref.UID]; exists && previous != evidence {
				bad[ref.UID] = true
			}
			if ref.UID == root.UID && key != rootKey {
				bad[ref.UID] = true
			}
			if previous, exists := names[key]; exists && previous != ref.UID {
				bad[ref.UID] = true
				bad[previous] = true
			}
			seen[ref.UID] = evidence
			names[key] = ref.UID
		}
	}
	clean := snapshot
	clean.Sources = make([]cr.RelationSourceResult, len(snapshot.Sources))
	for i, source := range snapshot.Sources {
		clean.Sources[i] = source
		clean.Sources[i].Objects = []cr.RelationObject{}
		for _, object := range source.Objects {
			if !bad[object.Resource.UID] {
				clean.Sources[i].Objects = append(clean.Sources[i].Objects, object)
			}
		}
	}
	return clean, len(bad) > 0
}

func relationIdentityKey(ref cr.ResourceRef) string {
	return strings.Join([]string{ref.ClusterID, relationAPIGroup(ref.APIVersion), ref.Kind, ref.Namespace, ref.Name}, "/")
}
