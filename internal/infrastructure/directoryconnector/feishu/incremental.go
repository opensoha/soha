package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type feishuEventObject struct {
	DepartmentID       string    `json:"department_id"`
	OpenDepartmentID   string    `json:"open_department_id"`
	UserID             string    `json:"user_id"`
	OpenID             string    `json:"open_id"`
	UnionID            string    `json:"union_id"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	Mobile             string    `json:"mobile"`
	ParentDepartmentID *string   `json:"parent_department_id"`
	DepartmentIDs      *[]string `json:"department_ids"`
	Avatar             struct {
		AvatarOrigin string `json:"avatar_origin"`
		Avatar640    string `json:"avatar_640"`
	} `json:"avatar"`
	Status *struct {
		IsFrozen    bool `json:"is_frozen"`
		IsResigned  bool `json:"is_resigned"`
		IsActivated bool `json:"is_activated"`
	} `json:"status"`
}

func (a *Adapter) ResolveDelta(ctx context.Context, connection domain.Connection, event domain.EventEnvelope) (domain.Delta, error) {
	kind, action := feishuEventAction(event.EventType)
	if kind == "" {
		return domain.Delta{}, fmt.Errorf("%w: unsupported Feishu event %q", domain.ErrReconcileRequired, event.EventType)
	}
	var payload struct {
		Object feishuEventObject `json:"object"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return domain.Delta{}, fmt.Errorf("decode Feishu event object: %w", err)
	}
	if kind == "organization" {
		return a.resolveOrganizationDelta(ctx, connection, action, event.OccurredAt, payload.Object)
	}
	return a.resolvePersonDelta(ctx, connection, action, event.OccurredAt, payload.Object)
}

func (a *Adapter) resolveOrganizationDelta(ctx context.Context, connection domain.Connection, action string, occurredAt time.Time, object feishuEventObject) (domain.Delta, error) {
	externalID := firstNonEmpty(object.OpenDepartmentID, object.DepartmentID)
	if externalID == "" {
		return domain.Delta{}, fmt.Errorf("%w: Feishu department ID is missing", domain.ErrReconcileRequired)
	}
	item := domain.Organization{ConnectionID: connection.ID, ExternalID: externalID, Status: domain.ProjectionArchived, LastSeenAt: occurredAt, ArchivedAt: &occurredAt}
	if action != domain.ChangeArchive {
		if object.Name != "" && object.ParentDepartmentID != nil {
			item.ExternalParentID, item.Name = strings.TrimSpace(*object.ParentDepartmentID), object.Name
			if item.ExternalParentID == "0" {
				item.ExternalParentID = ""
			}
		} else {
			client, err := a.client(ctx, connection)
			if err != nil {
				return domain.Delta{}, err
			}
			remote, err := client.GetOrganization(ctx, externalID)
			if err != nil {
				return domain.Delta{}, err
			}
			item.ExternalParentID, item.Name = remote.ParentExternalID, remote.Name
		}
		item.Status, item.ArchivedAt = domain.ProjectionActive, nil
	}
	return domain.Delta{Action: action, OccurredAt: occurredAt, Organization: &item}, nil
}

func (a *Adapter) resolvePersonDelta(ctx context.Context, connection domain.Connection, action string, occurredAt time.Time, object feishuEventObject) (domain.Delta, error) {
	delta := domain.Delta{Action: action, OccurredAt: occurredAt}
	externalID := firstNonEmpty(object.OpenID, object.UserID)
	if externalID == "" {
		return domain.Delta{}, fmt.Errorf("%w: Feishu user ID is missing", domain.ErrReconcileRequired)
	}
	person := domain.Person{ConnectionID: connection.ID, ExternalID: externalID, ProviderSubject: externalID, Status: domain.ProjectionArchived, LastSeenAt: occurredAt, ArchivedAt: &occurredAt, DepartedAt: &occurredAt}
	if action != domain.ChangeArchive {
		active := false
		var departmentIDs []string
		if object.Name != "" && object.DepartmentIDs != nil && object.Status != nil {
			person.ProviderSubject = firstNonEmpty(object.OpenID, object.UserID, externalID)
			person.DisplayName, person.Email, person.Phone = object.Name, object.Email, object.Mobile
			person.AvatarURL = firstNonEmpty(object.Avatar.AvatarOrigin, object.Avatar.Avatar640)
			active = object.Status.IsActivated && !object.Status.IsFrozen && !object.Status.IsResigned
			departmentIDs = *object.DepartmentIDs
		} else {
			client, err := a.client(ctx, connection)
			if err != nil {
				return domain.Delta{}, err
			}
			remote, memberships, err := client.GetPerson(ctx, externalID)
			if err != nil {
				return domain.Delta{}, err
			}
			person.ProviderSubject, person.DisplayName, person.Email, person.Phone, person.AvatarURL = remote.ProviderSubject, remote.Name, remote.Email, remote.Mobile, remote.AvatarURL
			active = remote.Active
			for _, membership := range memberships {
				departmentIDs = append(departmentIDs, membership.OrganizationExternalID)
			}
		}
		person.Status, person.ArchivedAt, person.DepartedAt = personStatus(active), nil, nil
		if !active {
			person.DepartedAt = &occurredAt
		}
		for _, departmentID := range departmentIDs {
			if departmentID != "" {
				delta.Memberships = append(delta.Memberships, domain.Membership{ConnectionID: connection.ID, ExternalPersonID: externalID, ExternalOrganizationID: departmentID, Status: domain.ProjectionActive, LastSeenAt: occurredAt})
			}
		}
	}
	delta.Person = &person
	return delta, nil
}

func feishuEventAction(eventType string) (string, string) {
	value := strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case strings.Contains(value, "department.created"):
		return "organization", domain.ChangeCreate
	case strings.Contains(value, "department.updated"):
		return "organization", domain.ChangeUpdate
	case strings.Contains(value, "department.deleted"):
		return "organization", domain.ChangeArchive
	case strings.Contains(value, "user.created"):
		return "person", domain.ChangeCreate
	case strings.Contains(value, "user.updated"):
		return "person", domain.ChangeUpdate
	case strings.Contains(value, "user.deleted") || strings.Contains(value, "user.resigned"):
		return "person", domain.ChangeArchive
	default:
		return "", ""
	}
}
