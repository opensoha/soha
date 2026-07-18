package directorysync

import (
	"fmt"
	"sort"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

func BuildPlan(snapshot domain.Snapshot, existingOrganizations []domain.Organization, existingPeople []domain.Person, includePeople bool) (domain.Plan, error) {
	organizations, err := parentFirst(snapshot.Organizations)
	if err != nil {
		return domain.Plan{}, err
	}
	plan := domain.Plan{
		Organizations: organizations,
		Changes:       buildOrganizationChanges(organizations, existingOrganizations),
	}
	if !includePeople {
		if len(snapshot.People) != 0 || len(snapshot.Memberships) != 0 {
			return domain.Plan{}, domain.ErrPeopleSyncDisabled
		}
		return plan, nil
	}
	people, changes, err := buildPeopleChanges(snapshot.People, existingPeople)
	if err != nil {
		return domain.Plan{}, err
	}
	plan.People = people
	plan.Memberships = snapshot.Memberships
	plan.Changes = append(plan.Changes, changes...)
	return plan, nil
}

func buildOrganizationChanges(organizations, existing []domain.Organization) []domain.Change {
	changes := make([]domain.Change, 0, len(organizations)+len(existing))
	orgs := indexOrganizations(existing)
	seenOrgs := make(map[string]bool, len(organizations))
	for _, item := range organizations {
		seenOrgs[item.ExternalID] = true
		old, ok := orgs[item.ExternalID]
		changes = append(changes, domain.Change{Kind: "organization", Action: organizationChangeAction(item, old, ok), ExternalID: item.ExternalID})
	}
	for _, old := range existing {
		if !seenOrgs[old.ExternalID] && old.Status != domain.ProjectionArchived {
			changes = append(changes, domain.Change{Kind: "organization", Action: domain.ChangeArchive, ExternalID: old.ExternalID})
		}
	}
	return changes
}

func organizationChangeAction(item, old domain.Organization, exists bool) string {
	if !exists {
		return domain.ChangeCreate
	}
	switch {
	case old.Status == domain.ProjectionArchived && item.Status == domain.ProjectionActive:
		return domain.ChangeRestore
	case old.ExternalParentID != item.ExternalParentID:
		return domain.ChangeMove
	case old.RawHash == item.RawHash && old.Status == item.Status:
		return domain.ChangeNoop
	default:
		return domain.ChangeUpdate
	}
}

func buildPeopleChanges(snapshot, existing []domain.Person) ([]domain.Person, []domain.Change, error) {
	peopleByID := make(map[string]domain.Person, len(existing))
	for _, person := range existing {
		peopleByID[person.ExternalID] = person
	}
	people := snapshot
	changes := make([]domain.Change, 0, len(snapshot)+len(existing))
	seenPeople := make(map[string]bool, len(snapshot))
	for _, person := range snapshot {
		if strings.TrimSpace(person.ExternalID) == "" {
			return nil, nil, fmt.Errorf("%w: person external id is empty", domain.ErrInvalidSnapshot)
		}
		seenPeople[person.ExternalID] = true
		old, ok := peopleByID[person.ExternalID]
		changes = append(changes, domain.Change{Kind: "person", Action: personChangeAction(person, old, ok), ExternalID: person.ExternalID})
	}
	for _, old := range existing {
		if !seenPeople[old.ExternalID] && old.Status != domain.ProjectionArchived {
			changes = append(changes, domain.Change{Kind: "person", Action: domain.ChangeArchive, ExternalID: old.ExternalID})
			old.Status = domain.ProjectionArchived
			people = append(people, old)
		}
	}
	return people, changes, nil
}

func personChangeAction(item, old domain.Person, exists bool) string {
	if !exists {
		return domain.ChangeCreate
	}
	if old.Status == domain.ProjectionArchived && item.Status == domain.ProjectionActive {
		return domain.ChangeRestore
	}
	if old.RawHash == item.RawHash && old.Status == item.Status {
		return domain.ChangeNoop
	}
	return domain.ChangeUpdate
}

func parentFirst(items []domain.Organization) ([]domain.Organization, error) {
	byID := make(map[string]domain.Organization, len(items))
	for _, item := range items {
		item.ExternalID = strings.TrimSpace(item.ExternalID)
		item.ExternalParentID = strings.TrimSpace(item.ExternalParentID)
		if item.ExternalID == "" {
			return nil, fmt.Errorf("%w: organization external id is empty", domain.ErrInvalidSnapshot)
		}
		if _, duplicate := byID[item.ExternalID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate organization %s", domain.ErrInvalidSnapshot, item.ExternalID)
		}
		byID[item.ExternalID] = item
	}
	children := make(map[string][]string)
	indegree := make(map[string]int, len(items))
	for id, item := range byID {
		indegree[id] = 0
		if item.ExternalParentID == "" {
			continue
		}
		if _, ok := byID[item.ExternalParentID]; !ok {
			return nil, fmt.Errorf("%w: organization %s references missing parent %s", domain.ErrInvalidSnapshot, id, item.ExternalParentID)
		}
		children[item.ExternalParentID] = append(children[item.ExternalParentID], id)
		indegree[id]++
	}
	queue := make([]string, 0)
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	result := make([]domain.Organization, 0, len(items))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, byID[id])
		sort.Strings(children[id])
		for _, child := range children[id] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
			}
		}
		sort.Strings(queue)
	}
	if len(result) != len(items) {
		return nil, fmt.Errorf("%w: organization parent cycle", domain.ErrInvalidSnapshot)
	}
	return result, nil
}

func indexOrganizations(items []domain.Organization) map[string]domain.Organization {
	result := make(map[string]domain.Organization, len(items))
	for _, item := range items {
		result[item.ExternalID] = item
	}
	return result
}
