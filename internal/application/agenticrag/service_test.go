package agenticrag

import (
	"context"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type plannerStub struct{ plans []QueryPlan }

func (p *plannerStub) Plan(_ context.Context, _ string, round int, _ []domainknowledge.Citation) (QueryPlan, error) {
	return p.plans[round-1], nil
}

type retrieverStub struct{ calls int }

func (r *retrieverStub) Search(_ context.Context, _ domainidentity.Principal, request domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	r.calls++
	if r.calls > 1 {
		return domainknowledge.SearchResult{TraceID: "trace-2", NoAnswer: true}, nil
	}
	citation := domainknowledge.Citation{ID: "citation-1", DocumentID: "document-1", ChunkID: "chunk-1"}
	return domainknowledge.SearchResult{TraceID: "trace-1", Hits: []domainknowledge.SearchHit{{ChunkID: "chunk-1", DocumentID: "document-1", Citation: citation}}}, nil
}

func TestServiceStopsOnNoProgressAndPreservesCitations(t *testing.T) {
	retriever := &retrieverStub{}
	service, err := NewService(retriever, &plannerStub{plans: []QueryPlan{{Queries: []string{"checkout failure"}}, {Queries: []string{"checkout owner"}}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(t.Context(), domainidentity.Principal{}, []string{"kb-1"}, "diagnose checkout", 3, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "no_progress" || len(result.Citations) != 1 || len(result.Steps) != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceBoundsPlannerQueries(t *testing.T) {
	retriever := &retrieverStub{}
	service, _ := NewService(retriever, &plannerStub{plans: []QueryPlan{{Queries: []string{"a", "b", "c", "d", "e"}, StopReason: "sufficient_evidence"}}})
	result, err := service.Execute(t.Context(), domainidentity.Principal{}, []string{"kb-1"}, "goal", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps[0].Queries) != 4 || retriever.calls != 4 {
		t.Fatalf("result = %#v, calls = %d", result, retriever.calls)
	}
}
