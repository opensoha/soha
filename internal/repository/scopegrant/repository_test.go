package scopegrant

import (
	"errors"
	"slices"
	"testing"

	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestNormalizeInputDefaultsLegacyAndNormalizesPlatformLists(t *testing.T) {
	item := normalizeInput(domainscopegrant.Input{
		SubjectType: " user ", SubjectID: " user-1 ",
		ClusterIDs: []string{" cluster-a ", "cluster-a", ""},
		Namespaces: []string{" minio ", "ops"},
	})
	if item.ScopeType != domainscopegrant.ScopeTypeLegacy {
		t.Fatalf("ScopeType = %q, want legacy", item.ScopeType)
	}
	if !slices.Equal(item.ClusterIDs, []string{"cluster-a"}) || !slices.Equal(item.Namespaces, []string{"minio", "ops"}) {
		t.Fatalf("platform scope = clusters %v namespaces %v", item.ClusterIDs, item.Namespaces)
	}
}
