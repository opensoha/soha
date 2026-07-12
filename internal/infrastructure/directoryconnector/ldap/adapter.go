package ldap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type CredentialResolver interface {
	GetConnectionCredential(context.Context, string) (domain.ConnectionCredential, error)
}
type Adapter struct {
	credentials CredentialResolver
	dial        func(string, ...goldap.DialOpt) (*goldap.Conn, error)
}

func NewAdapter(credentials CredentialResolver) (*Adapter, error) {
	if credentials == nil {
		return nil, errors.New("LDAP credential resolver is required")
	}
	return &Adapter{credentials: credentials, dial: goldap.DialURL}, nil
}

func (a *Adapter) Validate(ctx context.Context, c domain.Connection) (domain.Capabilities, error) {
	conn, err := a.connect(ctx, c)
	if err == nil {
		conn.Close()
	}
	return domain.Capabilities{Organizations: true, People: true, Memberships: true}, err
}
func (a *Adapter) ListOrganizations(ctx context.Context, c domain.Connection) ([]domain.Organization, string, error) {
	conn, err := a.connect(ctx, c)
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	base := metadata(c, "organizationBaseDN", metadata(c, "baseDN", ""))
	filter := metadata(c, "organizationFilter", "(objectClass=organizationalUnit)")
	idAttr := metadata(c, "organizationIdAttribute", "entryUUID")
	nameAttr := metadata(c, "organizationNameAttribute", "ou")
	result, err := conn.Search(goldap.NewSearchRequest(base, goldap.ScopeWholeSubtree, goldap.NeverDerefAliases, 0, 0, false, filter, []string{idAttr, nameAttr}, nil))
	if err != nil {
		return nil, "", err
	}
	idByDN := map[string]string{}
	for _, entry := range result.Entries {
		id := entry.GetAttributeValue(idAttr)
		if id == "" {
			id = entry.DN
		}
		idByDN[normalizeDN(entry.DN)] = id
	}
	items := make([]domain.Organization, 0, len(result.Entries))
	for _, entry := range result.Entries {
		id := idByDN[normalizeDN(entry.DN)]
		parent := idByDN[normalizeDN(parentDN(entry.DN))]
		name := entry.GetAttributeValue(nameAttr)
		if name == "" {
			name = firstRDNValue(entry.DN)
		}
		items = append(items, domain.Organization{ConnectionID: c.ID, ExternalID: id, ExternalParentID: parent, Name: name, Status: domain.ProjectionActive})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ExternalID < items[j].ExternalID })
	return items, "", nil
}
func (a *Adapter) ListPeople(ctx context.Context, c domain.Connection) ([]domain.Person, error) {
	entries, err := a.people(ctx, c)
	if err != nil {
		return nil, err
	}
	idAttr := metadata(c, "personIdAttribute", "entryUUID")
	usernameAttr := metadata(c, "usernameAttribute", "uid")
	nameAttr := metadata(c, "displayNameAttribute", "cn")
	emailAttr := metadata(c, "emailAttribute", "mail")
	phoneAttr := metadata(c, "phoneAttribute", "telephoneNumber")
	items := make([]domain.Person, 0, len(entries))
	for _, entry := range entries {
		id := entry.GetAttributeValue(idAttr)
		if id == "" {
			id = entry.DN
		}
		username := entry.GetAttributeValue(usernameAttr)
		if username == "" {
			username = id
		}
		items = append(items, domain.Person{ConnectionID: c.ID, ExternalID: id, ProviderSubject: username, Username: username, DisplayName: entry.GetAttributeValue(nameAttr), Email: entry.GetAttributeValue(emailAttr), Phone: entry.GetAttributeValue(phoneAttr), Status: domain.ProjectionActive})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ExternalID < items[j].ExternalID })
	return items, nil
}
func (a *Adapter) ListMemberships(ctx context.Context, c domain.Connection) ([]domain.Membership, error) {
	organizations, _, err := a.ListOrganizations(ctx, c)
	if err != nil {
		return nil, err
	}
	conn, err := a.connect(ctx, c)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	base := metadata(c, "organizationBaseDN", metadata(c, "baseDN", ""))
	idAttr := metadata(c, "organizationIdAttribute", "entryUUID")
	orgResult, err := conn.Search(goldap.NewSearchRequest(base, goldap.ScopeWholeSubtree, goldap.NeverDerefAliases, 0, 0, false, metadata(c, "organizationFilter", "(objectClass=organizationalUnit)"), []string{idAttr}, nil))
	if err != nil {
		return nil, err
	}
	orgByDN := map[string]string{}
	for i, entry := range orgResult.Entries {
		id := entry.GetAttributeValue(idAttr)
		if id == "" {
			id = entry.DN
		}
		orgByDN[normalizeDN(entry.DN)] = id
		if i < len(organizations) && organizations[i].ExternalID == "" {
			orgByDN[normalizeDN(entry.DN)] = organizations[i].ExternalID
		}
	}
	people, err := a.people(ctx, c)
	if err != nil {
		return nil, err
	}
	personIDAttr := metadata(c, "personIdAttribute", "entryUUID")
	membershipAttr := metadata(c, "membershipAttribute", "memberOf")
	items := []domain.Membership{}
	for _, entry := range people {
		personID := entry.GetAttributeValue(personIDAttr)
		if personID == "" {
			personID = entry.DN
		}
		for _, orgDN := range entry.GetAttributeValues(membershipAttr) {
			if orgID := orgByDN[normalizeDN(orgDN)]; orgID != "" {
				items = append(items, domain.Membership{ConnectionID: c.ID, ExternalPersonID: personID, ExternalOrganizationID: orgID, Status: domain.ProjectionActive})
			}
		}
	}
	return items, nil
}
func (a *Adapter) people(ctx context.Context, c domain.Connection) ([]*goldap.Entry, error) {
	conn, err := a.connect(ctx, c)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	base := metadata(c, "peopleBaseDN", metadata(c, "baseDN", ""))
	attributes := []string{metadata(c, "personIdAttribute", "entryUUID"), metadata(c, "usernameAttribute", "uid"), metadata(c, "displayNameAttribute", "cn"), metadata(c, "emailAttribute", "mail"), metadata(c, "phoneAttribute", "telephoneNumber"), metadata(c, "membershipAttribute", "memberOf")}
	result, err := conn.Search(goldap.NewSearchRequest(base, goldap.ScopeWholeSubtree, goldap.NeverDerefAliases, 0, 0, false, metadata(c, "peopleFilter", "(objectClass=person)"), attributes, nil))
	if err != nil {
		return nil, err
	}
	return result.Entries, nil
}
func (a *Adapter) connect(ctx context.Context, c domain.Connection) (*goldap.Conn, error) {
	endpoint := metadata(c, "endpoint", "")
	parsed, err := url.Parse(endpoint)
	if err != nil || !(parsed.Scheme == "ldap" || parsed.Scheme == "ldaps") {
		return nil, errors.New("LDAP endpoint must use ldap or ldaps")
	}
	options := []goldap.DialOpt{}
	if parsed.Scheme == "ldaps" {
		options = append(options, goldap.DialWithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()}))
	}
	conn, err := a.dial(endpoint, options...)
	if err != nil {
		return nil, err
	}
	if metadataBool(c, "startTLS") && parsed.Scheme == "ldap" {
		if err := conn.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()}); err != nil {
			conn.Close()
			return nil, err
		}
	}
	credential, err := a.credentials.GetConnectionCredential(ctx, c.ID)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Bind(credential.Username, credential.Password); err != nil {
		conn.Close()
		return nil, fmt.Errorf("LDAP bind failed: %w", err)
	}
	return conn, nil
}
func metadata(c domain.Connection, key, fallback string) string {
	if value, ok := c.Metadata[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
func metadataBool(c domain.Connection, key string) bool {
	value, _ := c.Metadata[key].(bool)
	return value
}
func normalizeDN(value string) string {
	parsed, err := goldap.ParseDN(value)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(parsed.String())
}
func parentDN(value string) string {
	parsed, err := goldap.ParseDN(value)
	if err != nil || len(parsed.RDNs) < 2 {
		return ""
	}
	parsed.RDNs = parsed.RDNs[1:]
	return parsed.String()
}
func firstRDNValue(value string) string {
	parsed, err := goldap.ParseDN(value)
	if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
		return value
	}
	return parsed.RDNs[0].Attributes[0].Value
}

var _ appdirectorysync.Connector = (*Adapter)(nil)
