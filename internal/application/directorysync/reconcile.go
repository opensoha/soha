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
	plan := domain.Plan{Organizations: organizations}
	orgs := indexOrganizations(existingOrganizations)
	seenOrgs := make(map[string]bool, len(organizations))
	for _, item := range organizations {
		seenOrgs[item.ExternalID] = true
		old, ok := orgs[item.ExternalID]
		action := domain.ChangeCreate
		if ok {
			switch {
			case old.Status == domain.ProjectionArchived && item.Status == domain.ProjectionActive:
				action = domain.ChangeRestore
			case old.ExternalParentID != item.ExternalParentID:
				action = domain.ChangeMove
			case old.RawHash == item.RawHash && old.Status == item.Status:
				action = domain.ChangeNoop
			default:
				action = domain.ChangeUpdate
			}
		}
		plan.Changes = append(plan.Changes, domain.Change{Kind: "organization", Action: action, ExternalID: item.ExternalID})
	}
	for _, old := range existingOrganizations {
		if !seenOrgs[old.ExternalID] && old.Status != domain.ProjectionArchived {
			plan.Changes = append(plan.Changes, domain.Change{Kind: "organization", Action: domain.ChangeArchive, ExternalID: old.ExternalID})
		}
	}
	if !includePeople {
		if len(snapshot.People) != 0 || len(snapshot.Memberships) != 0 {
			return domain.Plan{}, domain.ErrPeopleSyncDisabled
		}
		return plan, nil
	}
	plan.People = snapshot.People
	plan.Memberships = snapshot.Memberships
	people := make(map[string]domain.Person, len(existingPeople))
	for _, person := range existingPeople {
		people[person.ExternalID] = person
	}
	seenPeople := make(map[string]bool, len(snapshot.People))
	for _, person := range snapshot.People {
		if strings.TrimSpace(person.ExternalID) == "" {
			return domain.Plan{}, fmt.Errorf("%w: person external id is empty", domain.ErrInvalidSnapshot)
		}
		seenPeople[person.ExternalID] = true
		action := domain.ChangeCreate
		if old, ok := people[person.ExternalID]; ok {
			if old.Status == domain.ProjectionArchived && person.Status == domain.ProjectionActive {
				action = domain.ChangeRestore
			} else if old.RawHash == person.RawHash && old.Status == person.Status {
				action = domain.ChangeNoop
			} else {
				action = domain.ChangeUpdate
			}
		}
		plan.Changes = append(plan.Changes, domain.Change{Kind: "person", Action: action, ExternalID: person.ExternalID})
	}
	for _, old := range existingPeople {
		if !seenPeople[old.ExternalID] && old.Status != domain.ProjectionArchived {
			plan.Changes = append(plan.Changes, domain.Change{Kind: "person", Action: domain.ChangeArchive, ExternalID: old.ExternalID})
			old.Status = domain.ProjectionArchived
			plan.People = append(plan.People, old)
		}
	}
	return plan, nil
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
