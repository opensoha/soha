package resourcebackend

import (
	"errors"
	"fmt"
	"testing"

	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResourceMutationConflictsKeepPublicConflictSemantics(t *testing.T) {
	kubeConflict := apierrors.NewConflict(schema.GroupResource{Resource: "customresourcedefinitions"}, "widgets.example", errors.New("UID changed"))
	for _, err := range []error{contractruntime.ErrResourceOwnership, kubeConflict, fmt.Errorf("delete: %w", kubeConflict)} {
		if !errors.Is(resourceMutationError(err), apperrors.ErrConflict) {
			t.Fatalf("mutation conflict lost public semantics: %v", err)
		}
	}
	other := errors.New("transport unavailable")
	if resourceMutationError(nil) != nil || resourceMutationError(other) != other {
		t.Fatal("unrelated errors must retain their semantics")
	}
}
