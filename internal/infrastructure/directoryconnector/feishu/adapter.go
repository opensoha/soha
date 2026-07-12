package feishu

import (
	"context"
	"errors"
	"fmt"
	"sort"

	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

// TokenResolver resolves an opaque credential reference at call time. Implementations
// must not return or persist the token anywhere except the caller's memory.
type TokenResolver func(context.Context, domain.Connection) (string, error)

type Adapter struct {
	resolveToken TokenResolver
	options      []Option
}

func NewAdapter(resolveToken TokenResolver, options ...Option) (*Adapter, error) {
	if resolveToken == nil {
		return nil, errors.New("Feishu token resolver is required")
	}
	return &Adapter{resolveToken: resolveToken, options: options}, nil
}

func (a *Adapter) Validate(ctx context.Context, connection domain.Connection) (domain.Capabilities, error) {
	client, err := a.client(ctx, connection)
	if err != nil {
		return domain.Capabilities{}, err
	}
	if err := client.Validate(ctx); err != nil {
		return domain.Capabilities{}, err
	}
	return domain.Capabilities{Organizations: true, People: true, Memberships: true, Events: true}, nil
}

func (a *Adapter) ListOrganizations(ctx context.Context, connection domain.Connection) ([]domain.Organization, string, error) {
	client, err := a.client(ctx, connection)
	if err != nil {
		return nil, "", err
	}
	var result []domain.Organization
	for token := ""; ; {
		page, err := client.ListOrganizations(ctx, token)
		if err != nil {
			return nil, token, err
		}
		for _, item := range page.Items {
			result = append(result, domain.Organization{ConnectionID: connection.ID, ExternalID: item.ExternalID, ExternalParentID: item.ParentExternalID, Name: item.Name, Status: domain.ProjectionActive, SourceVersion: item.SourceVersion})
		}
		if page.NextToken == "" {
			return result, "", nil
		}
		token = page.NextToken
	}
}

func (a *Adapter) ListPeople(ctx context.Context, connection domain.Connection) ([]domain.Person, error) {
	client, organizations, err := a.clientAndOrganizations(ctx, connection)
	if err != nil {
		return nil, err
	}
	people := make(map[string]domain.Person)
	for _, organization := range organizations {
		for token := ""; ; {
			page, err := client.ListPeople(ctx, organization.ExternalID, token)
			if err != nil {
				return nil, err
			}
			for _, item := range page.Items {
				people[item.ExternalID] = domain.Person{ConnectionID: connection.ID, ExternalID: item.ExternalID, ProviderSubject: item.ProviderSubject, DisplayName: item.Name, Email: item.Email, Phone: item.Mobile, AvatarURL: item.AvatarURL, Status: personStatus(item.Active), SourceVersion: item.SourceVersion}
			}
			if page.NextToken == "" {
				break
			}
			token = page.NextToken
		}
	}
	ids := make([]string, 0, len(people))
	for id := range people {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]domain.Person, 0, len(ids))
	for _, id := range ids {
		result = append(result, people[id])
	}
	return result, nil
}

func (a *Adapter) ListMemberships(ctx context.Context, connection domain.Connection) ([]domain.Membership, error) {
	client, organizations, err := a.clientAndOrganizations(ctx, connection)
	if err != nil {
		return nil, err
	}
	memberships := make(map[string]domain.Membership)
	for _, organization := range organizations {
		for token := ""; ; {
			page, err := client.ListMemberships(ctx, organization.ExternalID, token)
			if err != nil {
				return nil, err
			}
			for _, item := range page.Items {
				key := item.PersonExternalID + "\x00" + item.OrganizationExternalID
				memberships[key] = domain.Membership{ConnectionID: connection.ID, ExternalPersonID: item.PersonExternalID, ExternalOrganizationID: item.OrganizationExternalID, Status: domain.ProjectionActive}
			}
			if page.NextToken == "" {
				break
			}
			token = page.NextToken
		}
	}
	keys := make([]string, 0, len(memberships))
	for key := range memberships {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]domain.Membership, 0, len(keys))
	for _, key := range keys {
		result = append(result, memberships[key])
	}
	return result, nil
}

func (a *Adapter) clientAndOrganizations(ctx context.Context, connection domain.Connection) (*Client, []domain.Organization, error) {
	client, err := a.client(ctx, connection)
	if err != nil {
		return nil, nil, err
	}
	organizations, _, err := a.ListOrganizations(ctx, connection)
	return client, organizations, err
}

func (a *Adapter) client(ctx context.Context, connection domain.Connection) (*Client, error) {
	if connection.ProviderType != "" && connection.ProviderType != domain.ProviderFeishu {
		return nil, fmt.Errorf("Feishu adapter cannot handle provider %q", connection.ProviderType)
	}
	token, err := a.resolveToken(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("resolve Feishu credential reference: %w", err)
	}
	return New(token, a.options...)
}

func personStatus(active bool) string {
	if active {
		return domain.ProjectionActive
	}
	return domain.ProjectionSuspended
}

var _ appdirectorysync.Connector = (*Adapter)(nil)
