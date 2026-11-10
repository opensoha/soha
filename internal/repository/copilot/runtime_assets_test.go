package copilot

import (
	"slices"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func TestRuntimeAssetStateWithPostgres(t *testing.T) {
	repo, _, ctx := newCopilotPostgresTestRepository(t)
	v1 := domain.RuntimeAssetRef{Kind: "mcp", ID: "example", Version: "1"}
	v2 := v1
	v2.Version = "2"
	skill := v1
	skill.Kind = "skill"
	refs := []domain.RuntimeAssetRef{v1, v2, skill}
	assertDisabled := func(want []domain.RuntimeAssetRef) {
		t.Helper()
		got, err := repo.DisabledRuntimeAssets(ctx, refs)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("disabled: %+v %v, want %+v", got, err, want)
		}
	}
	assertDisabled(nil)
	if err := repo.SetRuntimeAssetEnabled(ctx, "operator", v1, false); err != nil {
		t.Fatal(err)
	}
	assertDisabled([]domain.RuntimeAssetRef{v1})
	if err := repo.SetRuntimeAssetEnabled(ctx, "operator", v1, true); err != nil {
		t.Fatal(err)
	}
	assertDisabled(nil)
}
