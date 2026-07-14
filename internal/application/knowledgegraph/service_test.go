package knowledgegraph

import (
	"testing"
)

func TestRevisionRequiresProvenanceAndPublishesAtomically(t *testing.T) {
	store := NewMemoryStore()
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	revision := Revision{ID: "graph-1", KnowledgeBaseID: "kb-1", SourceIndexRef: "index-1", ExtractorVer: "v1", Entities: []Entity{{ID: "service", Name: "Checkout Service", SourceRefs: []string{"document:1"}}}, Communities: []Community{{ID: "community-1", EntityIDs: []string{"service"}, Summary: "Checkout ownership", SourceRefs: []string{"document:1"}}}}
	if err := service.PutRevision(t.Context(), revision); err != nil {
		t.Fatal(err)
	}
	published, err := service.Publish(t.Context(), revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != "active" {
		t.Fatalf("published = %#v", published)
	}
	result, err := service.Query(t.Context(), revision.ID, "checkout", "local", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoAnswer || len(result.Entities) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRevisionRejectsRelationWithoutProvenance(t *testing.T) {
	service, _ := NewService(NewMemoryStore())
	err := service.PutRevision(t.Context(), Revision{ID: "graph-1", KnowledgeBaseID: "kb-1", SourceIndexRef: "index-1", ExtractorVer: "v1", Entities: []Entity{{ID: "a", Name: "A", SourceRefs: []string{"document:1"}}, {ID: "b", Name: "B", SourceRefs: []string{"document:1"}}}, Relations: []Relation{{ID: "r", FromEntityID: "a", ToEntityID: "b", Confidence: 1}}})
	if err == nil {
		t.Fatal("expected missing provenance error")
	}
}
