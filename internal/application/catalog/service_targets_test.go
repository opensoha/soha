package catalog

import (
	"context"
	"errors"
	"testing"

	dc "github.com/opensoha/soha/internal/domain/catalog"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestOmittedTargetsPreserveValidationButNotReplacementIntent(t *testing.T) {
	current := dc.ApplicationEnvironment{Targets: []dc.ReleaseTarget{{ID: "t", ClusterID: "actual", Namespace: "orders", ExecutorKind: "observe_only", Metadata: map[string]any{"resourceUid": "uid", "serviceId": "logical"}}}}
	input := dc.ApplicationEnvironmentInput{ClusterID: "new-default", Namespace: "default"}
	validation := validationTargets(input, current)
	if input.Targets != nil || len(validation.Targets) != 1 || validation.Targets[0].ClusterID != "actual" || validation.Targets[0].Namespace != "orders" || validation.Targets[0].ExecutorKind != "observe_only" {
		t.Fatalf("validation=%+v", validation)
	}
	explicit := dc.ApplicationEnvironmentInput{Targets: []dc.ReleaseTargetInput{}}
	if len(validationTargets(explicit, current).Targets) != 0 {
		t.Fatal("explicit empty replacement was ignored")
	}
}

func TestTargetMappingRejectsAmbiguousOrUnauthorizedIdentity(t *testing.T) {
	service := &Service{}
	_, err := service.prepareTargetServiceMappings(context.Background(), di.Principal{}, dc.ApplicationEnvironmentInput{ApplicationID: "a", Targets: []dc.ReleaseTargetInput{{ServiceID: "s", Metadata: map[string]any{"serviceId": "other"}}}}, dc.ApplicationEnvironment{})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("conflict=%v", err)
	}
	_, err = service.prepareTargetServiceMappings(context.Background(), di.Principal{}, dc.ApplicationEnvironmentInput{ApplicationID: "a", Targets: []dc.ReleaseTargetInput{{ServiceID: "s"}}}, dc.ApplicationEnvironment{})
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("missing authorizer=%v", err)
	}
	current := dc.ApplicationEnvironment{ApplicationID: "a", Targets: []dc.ReleaseTarget{{ID: "t", Metadata: map[string]any{"serviceId": "s"}}}}
	input, err := service.prepareTargetServiceMappings(context.Background(), di.Principal{}, dc.ApplicationEnvironmentInput{ApplicationID: "a", Targets: []dc.ReleaseTargetInput{{ID: "t"}}}, current)
	if err != nil || input.Targets[0].ServiceID != "s" || input.Targets[0].Metadata["serviceId"] != "s" {
		t.Fatalf("legacy=%+v %v", input, err)
	}
}
