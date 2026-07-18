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

func TestBuildPlanClassifiesOrganizationAndPersonChanges(t *testing.T) {
	snapshot := domain.Snapshot{
		Organizations: []domain.Organization{
			{ExternalID: "org-create", Status: domain.ProjectionActive},
			{ExternalID: "org-move", Status: domain.ProjectionActive},
			{ExternalID: "org-noop", Status: domain.ProjectionActive, RawHash: "same"},
			{ExternalID: "org-restore", Status: domain.ProjectionActive},
			{ExternalID: "org-update", Status: domain.ProjectionActive, RawHash: "new"},
		},
		People: []domain.Person{
			{ExternalID: "person-create", Status: domain.ProjectionActive},
			{ExternalID: "person-noop", Status: domain.ProjectionActive, RawHash: "same"},
			{ExternalID: "person-restore", Status: domain.ProjectionActive},
			{ExternalID: "person-update", Status: domain.ProjectionActive, RawHash: "new"},
		},
	}
	existingOrganizations := []domain.Organization{
		{ExternalID: "org-archive", Status: domain.ProjectionActive},
		{ExternalID: "org-move", ExternalParentID: "old-parent", Status: domain.ProjectionActive},
		{ExternalID: "org-noop", Status: domain.ProjectionActive, RawHash: "same"},
		{ExternalID: "org-restore", Status: domain.ProjectionArchived},
		{ExternalID: "org-update", Status: domain.ProjectionActive, RawHash: "old"},
	}
	existingPeople := []domain.Person{
		{ExternalID: "person-archive", Status: domain.ProjectionActive},
		{ExternalID: "person-noop", Status: domain.ProjectionActive, RawHash: "same"},
		{ExternalID: "person-restore", Status: domain.ProjectionArchived},
		{ExternalID: "person-update", Status: domain.ProjectionActive, RawHash: "old"},
	}

	plan, err := BuildPlan(snapshot, existingOrganizations, existingPeople, true)
	if err != nil {
		t.Fatal(err)
	}
	actions := make(map[string]string, len(plan.Changes))
	for _, change := range plan.Changes {
		actions[change.Kind+":"+change.ExternalID] = change.Action
	}
	wantActions := map[string]string{
		"organization:org-create":  domain.ChangeCreate,
		"organization:org-move":    domain.ChangeMove,
		"organization:org-noop":    domain.ChangeNoop,
		"organization:org-restore": domain.ChangeRestore,
		"organization:org-update":  domain.ChangeUpdate,
		"organization:org-archive": domain.ChangeArchive,
		"person:person-create":     domain.ChangeCreate,
		"person:person-noop":       domain.ChangeNoop,
		"person:person-restore":    domain.ChangeRestore,
		"person:person-update":     domain.ChangeUpdate,
		"person:person-archive":    domain.ChangeArchive,
	}
	for key, want := range wantActions {
		if got := actions[key]; got != want {
			t.Errorf("action %s = %q, want %q", key, got, want)
		}
	}
	if len(actions) != len(wantActions) {
		t.Fatalf("actions = %#v, want %#v", actions, wantActions)
	}
	archivedPerson := plan.People[len(plan.People)-1]
	if archivedPerson.ExternalID != "person-archive" || archivedPerson.Status != domain.ProjectionArchived {
		t.Fatalf("archived person projection = %#v", archivedPerson)
	}
}

func TestBuildPlanRejectsPersonWithoutExternalID(t *testing.T) {
	_, err := BuildPlan(domain.Snapshot{People: []domain.Person{{ExternalID: " "}}}, nil, nil, true)
	if !errors.Is(err, domain.ErrInvalidSnapshot) {
		t.Fatalf("BuildPlan() error = %v, want ErrInvalidSnapshot", err)
	}
}
