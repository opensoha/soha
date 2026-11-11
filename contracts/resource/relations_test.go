package resource

import "testing"

func TestRelationConfigSupportsUnknownKindsWithoutCodeRegistration(t *testing.T) {
	source := RelationSource{APIVersion: "widgets.example.test/v1", Resource: "widgets", Kind: "Widget", Namespace: "team"}
	config := RelationConfig{Version: "v1", RootUID: "root", Sources: []RelationSource{source}, Rules: []RelationRule{{ID: "shared", Type: "reference", Target: source, Name: "runtime"}}}
	if err := ValidateRelationConfig(config); err != nil {
		t.Fatal(err)
	}
	config.Rules[0].Type = "manual"
	if ValidateRelationConfig(config) == nil {
		t.Fatal("manual association accepted without UID")
	}
	config.Rules[0].UID = "fixed-runtime"
	if err := ValidateRelationConfig(config); err != nil {
		t.Fatal(err)
	}
	config.Rules[0].Target.Resource = "widgets/../../secrets"
	if ValidateRelationConfig(config) == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestRelationQueryBoundsAndSelectors(t *testing.T) {
	q := RelationQuery{ID: "pods", Source: RelationSource{APIVersion: "v1", Resource: "pods", Kind: "Pod", Namespace: "team"}, Selector: map[string]string{"app": "demo"}}
	for _, test := range []struct {
		name   string
		change func(*RelationQuery)
	}{
		{"all namespaces", func(q *RelationQuery) { q.Source.Namespace = "" }},
		{"selector injection", func(q *RelationQuery) { q.Selector["app"] = "demo,secret=yes" }},
		{"named selector", func(q *RelationQuery) { q.Name = "pod" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := q
			copy.Selector = map[string]string{"app": "demo"}
			test.change(&copy)
			if ValidateRelationSnapshotRequest(RelationSnapshotRequest{Queries: []RelationQuery{copy}}) == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
	if ValidateRelationSnapshotRequest(RelationSnapshotRequest{Queries: []RelationQuery{q, q}}) == nil {
		t.Fatal("duplicate query accepted")
	}
}
