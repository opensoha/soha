package directorysync

import (
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

func TestBuildPlanOrdersParentsFirst(t *testing.T) {
	plan, err := BuildPlan(domain.Snapshot{Organizations: []domain.Organization{{ExternalID: "child", ExternalParentID: "root"}, {ExternalID: "root"}}}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Organizations[0].ExternalID != "root" || plan.Organizations[1].ExternalID != "child" {
		t.Fatalf("wrong order: %+v", plan.Organizations)
	}
}

func TestBuildPlanRejectsPeopleWhenGateClosed(t *testing.T) {
	_, err := BuildPlan(domain.Snapshot{People: []domain.Person{{ExternalID: "p1"}}}, nil, nil, false)
	if !errors.Is(err, domain.ErrPeopleSyncDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildPlanRejectsMissingParentAndCycle(t *testing.T) {
	for _, organizations := range [][]domain.Organization{{{ExternalID: "child", ExternalParentID: "missing"}}, {{ExternalID: "a", ExternalParentID: "b"}, {ExternalID: "b", ExternalParentID: "a"}}} {
		if _, err := BuildPlan(domain.Snapshot{Organizations: organizations}, nil, nil, false); !errors.Is(err, domain.ErrInvalidSnapshot) {
			t.Fatalf("got %v", err)
		}
	}
}
