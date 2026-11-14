package delivery

import (
	"context"
	"errors"
	cr "github.com/opensoha/soha/contracts/resource"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type relationViewTargetStub struct {
	stubTargetReader
	source *dr.ResourceSourceSnapshot
	calls  *int
}

func (s relationViewTargetStub) GetApplicationResourceRelations(_ context.Context, _ di.Principal, source dr.ResourceSourceSnapshot, config cr.RelationConfig) (cr.ResourceRelationsView, error) {
	*s.source = source
	*s.calls++
	return cr.ResourceRelationsView{Root: source.Resource, Config: config}, nil
}
func TestImportedRelationsUseOnlyAuthorizedBindingTarget(t *testing.T) {
	source := identityImportSource("Deployment")
	target := linkedSourceTarget(t, source)
	var captured dr.ResourceSourceSnapshot
	calls := 0
	reader := relationViewTargetStub{source: &captured, calls: &calls}
	service := New(stubApplicationReader{}, stubCatalogReader{bindings: []dc.ApplicationEnvironment{{ID: "binding", ApplicationID: "app", Targets: []dc.ReleaseTarget{target}}}}, nil, nil, nil, stubRepository{}, nil, reader, nil)
	if _, err := service.GetImportedTargetRelations(context.Background(), deliveryActionPrincipal(), "binding", "foreign"); !errors.Is(err, apperrors.ErrNotFound) || calls != 0 {
		t.Fatalf("foreign target read: %v calls=%d", err, calls)
	}
	if _, err := service.GetImportedTargetRelations(context.Background(), deliveryActionPrincipal(), "binding", "target"); err != nil || captured.Resource.UID != "uid-1" || calls != 1 {
		t.Fatalf("identity=%+v err=%v", captured, err)
	}
	target.Metadata["resourceRelations"] = map[string]any{"version": "v1", "rootUid": "uid-1", "sources": []any{}, "rules": []any{}, "script": "deny"}
	if _, err := service.GetImportedTargetRelations(context.Background(), deliveryActionPrincipal(), "binding", "target"); !errors.Is(err, apperrors.ErrInvalidArgument) || calls != 1 {
		t.Fatalf("unknown rules reached reader: %v", err)
	}
}
