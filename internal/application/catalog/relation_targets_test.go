package catalog

import (
	cr "github.com/opensoha/soha-contracts/resource"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	"testing"
	"time"
)

func TestRelationTargetsValidatePersistedDeclarativeRules(t *testing.T) {
	config := cr.RelationConfig{Version: "v1", RootUID: "root", Sources: []cr.RelationSource{}, Rules: []cr.RelationRule{}}
	target := dc.ReleaseTargetInput{Metadata: map[string]any{"importSource": "kubernetes", "resourceSource": `{"resource":{"uid":"root"}}`, "resourceRelations": config}}
	if err := validateRelationTargets([]dc.ReleaseTargetInput{target}); err != nil {
		t.Fatal(err)
	}
	config.RootUID = "replacement"
	target.Metadata["resourceRelations"] = config
	if validateRelationTargets([]dc.ReleaseTargetInput{target}) == nil {
		t.Fatal("rules reassigned to a different root")
	}
	target.Metadata["resourceRelations"] = map[string]any{"version": "v1", "rootUid": "root", "sources": []any{}, "rules": []any{}, "script": "deny"}
	if validateRelationTargets([]dc.ReleaseTargetInput{target}) == nil {
		t.Fatal("executable or unknown configuration accepted")
	}
}

func TestRelationBindingReplacementAlwaysRequiresRevision(t *testing.T) {
	target := dc.ReleaseTarget{ID: "target", Metadata: map[string]any{"resourceRelations": map[string]any{"rootUid": "root"}}}
	current := dc.ApplicationEnvironment{Targets: []dc.ReleaseTarget{target}}
	input := dc.ApplicationEnvironmentInput{Targets: []dc.ReleaseTargetInput{{ID: target.ID, Metadata: target.Metadata}}}
	if validateRelationRuleRevision(input, current) == nil {
		t.Fatal("unchanged rules allowed unfenced full replacement")
	}
	input.Targets = nil
	if validateRelationRuleRevision(input, current) == nil {
		t.Fatal("removal allowed without revision")
	}
	revision := time.Now()
	input.ExpectedUpdatedAt = &revision
	if err := validateRelationRuleRevision(input, current); err != nil {
		t.Fatal(err)
	}
	input = dc.ApplicationEnvironmentInput{}
	if err := validateRelationRuleRevision(input, dc.ApplicationEnvironment{}); err != nil {
		t.Fatal("legacy binding without rules must remain compatible", err)
	}
}
