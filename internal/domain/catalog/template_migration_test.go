package catalog

import (
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
	"testing"
)

func TestMigrationVersionBoundaries(t *testing.T) {
	ordered := &sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "backup then migrate"}
	irreversible := &sohaapi.TemplateMigrationPolicy{Irreversible: true, Guide: "forward recovery only"}
	versions := []MigrationVersion{{Number: 1, Digest: "a"}, {Number: 2, Digest: "b", Policy: ordered}, {Number: 3, Digest: "c", Policy: irreversible}, {Number: 4, Digest: "d"}}
	for _, tc := range []struct {
		name              string
		from, to          int64
		blocked, approval bool
		contains          string
	}{
		{"skip-required-boundary", 1, 4, true, true, "v2"},
		{"adjacent-requires-review", 1, 2, false, true, "审批"},
		{"irreversible-forward-review", 2, 4, false, true, "审批"},
		{"irreversible-rollback", 4, 2, true, true, "不可逆"},
		{"reapply-same-version", 3, 3, false, false, ""},
		{"unknown-runtime-is-not-adoption", 0, 4, true, false, "基线未知"},
		{"ordinary-after-boundaries", 3, 4, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessMigration(versions, tc.from, tc.to)
			if got == nil || got.Blocked != tc.blocked || got.RequiresApproval != tc.approval || !strings.Contains(got.Reason, tc.contains) {
				t.Fatalf("assessment %#v", got)
			}
		})
	}
	gap := []MigrationVersion{versions[0], versions[1], versions[3]}
	if got := AssessMigration(gap, 1, 4); got == nil || !got.Blocked || !strings.Contains(got.Reason, "历史缺失") {
		t.Fatalf("missing history %#v", got)
	}
	if got := AssessMigration([]MigrationVersion{{Number: 1}, {Number: 4}}, 1, 4); got != nil {
		t.Fatal("legacy templates changed behavior")
	}
	// Removing a declaration from the latest version cannot erase an earlier boundary.
	if got := AssessMigration(versions, 1, 4); len(got.Steps) != 2 || got.Steps[0].Version != 2 {
		t.Fatal("intermediate declarations lost")
	}
	if err := ValidateMigrationPolicy(&sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "  "}); err == nil {
		t.Fatal("blank guide accepted")
	}
	if err := ValidateMigrationPolicy(&sohaapi.TemplateMigrationPolicy{Irreversible: true, Guide: strings.Repeat("字", 2001)}); err == nil {
		t.Fatal("unbounded guide accepted")
	}
	if got := AssessMigration(versions, 1, 999); !got.Blocked || len(got.Steps) > 200 {
		t.Fatal("unbounded assessment")
	}
	ref := &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 2, TemplateDigest: "b"}
	if MigrationBaselineVersion(versions, "template", ref) != 2 {
		t.Fatal("verified baseline lost")
	}
	ref.TemplateDigest = "wrong"
	if MigrationBaselineVersion(versions, "template", ref) != 0 {
		t.Fatal("unverified digest accepted")
	}
}
