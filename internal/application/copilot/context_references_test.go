package copilot

import (
	"context"
	"errors"
	"strings"
	"testing"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type contextResourceReader struct{ agentToolResourceReader }

func (r contextResourceReader) ListPods(ctx context.Context, p domainidentity.Principal, cluster, namespace string) ([]domainresource.PodView, error) {
	if p.UserID != "reader" || cluster != "allowed" || namespace != "apps" {
		return nil, apperrors.ErrAccessDenied
	}
	return r.agentToolResourceReader.ListPods(ctx, p, cluster, namespace)
}

func TestChatReferencesReadAsUserAndClipAttachments(t *testing.T) {
	service := &Service{resources: contextResourceReader{agentToolResourceReader{pods: []domainresource.PodView{{Name: "api", Namespace: "apps", Phase: "Running"}}}}}
	selection := &domaincopilot.ContextSelection{
		References:  []domaincopilot.ContextReference{{Kind: "pod", ClusterID: "allowed", Namespace: "apps", Name: "api"}},
		Attachments: []domaincopilot.TextAttachment{{ID: "file-1", Name: "incident.log", Content: "password=do-not-send\n" + strings.Repeat("x", 30000)}},
	}
	metadata := domaincopilot.SessionMetadata{RequestContext: selection}
	ctx := context.Background()
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "other"}, "session", metadata, "inspect"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("resource ACL not enforced: %v", err)
	}
	envelope, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "reader"}, "session", metadata, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Evidence) != 2 || len(envelope.Truncations) != 1 || envelope.BudgetUsage.EvidenceTokens != 6000 || envelope.ContentHash == "" {
		t.Fatalf("missing budget/provenance: %+v", envelope)
	}
	if strings.Contains(envelope.Evidence[1].Content, "do-not-send") {
		t.Fatal("attachment secret was not redacted")
	}
	if envelope.Citations[0].URI != "resource:allowed/apps/pod/api" {
		t.Fatal("resource cannot be located")
	}
	metadata.RequestContext = nil
	if removed, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "reader"}, "session", metadata, "next"); err != nil || removed != nil {
		t.Fatal("removed references inherited")
	}
	for _, file := range []domaincopilot.TextAttachment{{ID: "x", Name: "test.pdf"}, {ID: "x", Name: "test.log", Content: "binary\x00data"}, {ID: "x", Name: "test.log", Content: strings.Repeat("x", 65537)}} {
		if validateChatAttachment(file) == nil {
			t.Fatalf("invalid attachment accepted: %s", file.Name)
		}
	}
}

type branchReferenceStore struct {
	*inspectionAuthzTestRepository
	archived bool
}

func (r *branchReferenceStore) GetSession(_ context.Context, owner, id string) (domaincopilot.Session, error) {
	if owner != "owner" || id != "source" {
		return domaincopilot.Session{}, apperrors.ErrAccessDenied
	}
	metadata := map[string]any{}
	if r.archived {
		metadata["archivedAt"] = "2026-09-10T00:00:00Z"
	}
	return domaincopilot.Session{ID: "source", CreatedBy: "owner", Title: "Original", Metadata: metadata}, nil
}
func (r *branchReferenceStore) GetMessage(_ context.Context, session, id string) (domaincopilot.Message, error) {
	if session != "source" || id != "old" {
		return domaincopilot.Message{}, apperrors.ErrNotFound
	}
	return domaincopilot.Message{ID: "old", SessionID: session, Role: "assistant", Content: "frozen selected background"}, nil
}
func TestBranchReferenceReauthorizesOriginalMessagesAndCanBeRemoved(t *testing.T) {
	store := &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}}
	service := &Service{sessions: store, messages: store}
	metadata := domaincopilot.SessionMetadata{PinnedContext: map[string]any{"branchReference": map[string]any{"kind": "session", "name": "Branch", "sessionId": "source", "messageIds": []string{"old"}}}}
	ctx := context.Background()
	envelope, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "owner"}, "branch", metadata, "continue")
	if err != nil || len(envelope.Evidence) != 1 || !strings.Contains(envelope.Evidence[0].Content, "frozen selected background") {
		t.Fatalf("branch evidence: %+v %v", envelope, err)
	}
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "other"}, "branch", metadata, "continue"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cross-user branch: %v", err)
	}
	store.archived = true
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "owner"}, "branch", metadata, "continue"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("archived source reused: %v", err)
	}
	metadata.PinnedContext = nil
	if envelope, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "owner"}, "branch", metadata, "continue"); err != nil || envelope != nil {
		t.Fatalf("removed branch reused: %+v %v", envelope, err)
	}
}
