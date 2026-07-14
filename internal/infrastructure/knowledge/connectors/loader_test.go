package connectors

import (
	"context"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestInlineAdapterRejectsUnboundedContent(t *testing.T) {
	t.Parallel()
	_, _, err := (InlineAdapter{}).Load(context.Background(), domainidentity.Principal{}, domainknowledge.Source{Config: map[string]any{"documents": []map[string]any{{"externalId": "doc", "content": string(make([]byte, maxInlineBytes+1))}}}})
	if err == nil {
		t.Fatal("expected inline content limit error")
	}
}

func TestLoaderRejectsUnregisteredNetworkSource(t *testing.T) {
	t.Parallel()
	_, _, err := DefaultLoader().Load(context.Background(), domainidentity.Principal{}, domainknowledge.Source{Kind: domainknowledge.SourceKindHTTP})
	if err == nil {
		t.Fatal("expected unavailable connector error")
	}
}
