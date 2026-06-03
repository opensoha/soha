package virtualization

import (
	"slices"
	"testing"

	domainvirtualization "github.com/soha/soha/internal/domain/virtualization"
)

func TestVMExtraClausesExcludeDeletedByDefault(t *testing.T) {
	clauses, args := vmExtraClauses(domainvirtualization.VMFilter{Namespace: "apps"})
	if !slices.Contains(clauses, "namespace = ?") {
		t.Fatalf("clauses = %#v, want namespace clause", clauses)
	}
	if !slices.Contains(clauses, "status <> ?") {
		t.Fatalf("clauses = %#v, want deleted exclusion", clauses)
	}
	if len(args) != 2 || args[0] != "apps" || args[1] != "deleted" {
		t.Fatalf("args = %#v", args)
	}
}

func TestVMExtraClausesAllowsExplicitDeletedStatus(t *testing.T) {
	clauses, args := vmExtraClauses(domainvirtualization.VMFilter{Status: "deleted"})
	if slices.Contains(clauses, "status <> ?") {
		t.Fatalf("clauses = %#v, should not add deleted exclusion", clauses)
	}
	if len(args) != 0 {
		t.Fatalf("args = %#v, want none", args)
	}
}
