package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type LoginProviderResolver interface {
	ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error)
}

type Adapter struct {
	resolver LoginProviderResolver
	client   *http.Client
	baseURL  string
	mu       sync.Mutex
	tokens   map[string]cachedToken
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

func NewAdapter(resolver LoginProviderResolver, client *http.Client, baseURL string) (*Adapter, error) {
	if resolver == nil {
		return nil, errors.New("WeCom login provider resolver is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://qyapi.weixin.qq.com"
	}
	return &Adapter{resolver: resolver, client: client, baseURL: strings.TrimRight(baseURL, "/"), tokens: map[string]cachedToken{}}, nil
}

func (a *Adapter) Validate(ctx context.Context, connection domain.Connection) (domain.Capabilities, error) {
	_, _, err := a.departments(ctx, connection)
	return domain.Capabilities{Organizations: true, People: true, Memberships: true, Events: true}, err
}

func (a *Adapter) ListOrganizations(ctx context.Context, connection domain.Connection) ([]domain.Organization, string, error) {
	departments, _, err := a.departments(ctx, connection)
	if err != nil {
		return nil, "", err
	}
	items := make([]domain.Organization, 0, len(departments))
	for _, item := range departments {
		parent := fmt.Sprint(item.ParentID)
		if item.ParentID == 0 || item.ParentID == 1 {
			parent = ""
		}
		items = append(items, domain.Organization{ConnectionID: connection.ID, ExternalID: fmt.Sprint(item.ID), ExternalParentID: parent, Name: item.Name, Status: domain.ProjectionActive, SourceVersion: fmt.Sprint(item.Order)})
	}
	return items, "", nil
}

func (a *Adapter) ListPeople(ctx context.Context, connection domain.Connection) ([]domain.Person, error) {
	users, err := a.users(ctx, connection)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Person, 0, len(users))
	for _, user := range users {
		status := domain.ProjectionActive
		if user.Status != 1 {
			status = domain.ProjectionSuspended
		}
		result = append(result, domain.Person{ConnectionID: connection.ID, ExternalID: user.UserID, ProviderSubject: user.UserID, Username: user.UserID, DisplayName: user.Name, Email: user.Email, Phone: user.Mobile, AvatarURL: user.Avatar, Status: status})
	}
	return result, nil
}

func (a *Adapter) ListMemberships(ctx context.Context, connection domain.Connection) ([]domain.Membership, error) {
	users, err := a.users(ctx, connection)
	if err != nil {
		return nil, err
	}
	result := []domain.Membership{}
	for _, user := range users {
		for _, departmentID := range user.Departments {
			result = append(result, domain.Membership{ConnectionID: connection.ID, ExternalPersonID: user.UserID, ExternalOrganizationID: fmt.Sprint(departmentID), Status: domain.ProjectionActive})
		}
	}
	return result, nil
}

type department struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID int64  `json:"parentid"`
	Order    int64  `json:"order"`
}

func (a *Adapter) departments(ctx context.Context, connection domain.Connection) ([]department, string, error) {
	token, err := a.token(ctx, connection)
	if err != nil {
		return nil, "", err
	}
	var response struct {
		ErrCode     int          `json:"errcode"`
		ErrMsg      string       `json:"errmsg"`
		Departments []department `json:"department"`
	}
	if err := a.get(ctx, "/cgi-bin/department/list", url.Values{"access_token": {token}}, &response); err != nil {
		return nil, "", err
	}
	if response.ErrCode != 0 {
		return nil, "", fmt.Errorf("WeCom department list failed: code=%d message=%q", response.ErrCode, response.ErrMsg)
	}
	sort.Slice(response.Departments, func(i, j int) bool { return response.Departments[i].ID < response.Departments[j].ID })
	return response.Departments, "", nil
}

type user struct {
	UserID      string  `json:"userid"`
	Name        string  `json:"name"`
	Mobile      string  `json:"mobile"`
	Email       string  `json:"email"`
	Avatar      string  `json:"avatar"`
	Status      int     `json:"status"`
	Departments []int64 `json:"department"`
}

func (a *Adapter) users(ctx context.Context, connection domain.Connection) ([]user, error) {
	token, err := a.token(ctx, connection)
	if err != nil {
		return nil, err
	}
	var response struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Users   []user `json:"userlist"`
	}
	if err := a.get(ctx, "/cgi-bin/user/list", url.Values{"access_token": {token}, "department_id": {"1"}, "fetch_child": {"1"}}, &response); err != nil {
		return nil, err
	}
	if response.ErrCode != 0 {
		return nil, fmt.Errorf("WeCom user list failed: code=%d message=%q", response.ErrCode, response.ErrMsg)
	}
	sort.Slice(response.Users, func(i, j int) bool { return response.Users[i].UserID < response.Users[j].UserID })
	return response.Users, nil
}

func (a *Adapter) token(ctx context.Context, connection domain.Connection) (string, error) {
	a.mu.Lock()
	cached := a.tokens[connection.LoginProviderID]
	if cached.value != "" && time.Until(cached.expiresAt) > time.Minute {
		a.mu.Unlock()
		return cached.value, nil
	}
	a.mu.Unlock()
	provider, err := a.resolver.ResolveLoginProvider(ctx, connection.LoginProviderID)
	if err != nil {
		return "", err
	}
	var response struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := a.get(ctx, "/cgi-bin/gettoken", url.Values{"corpid": {provider.ClientID}, "corpsecret": {provider.ClientSecret}}, &response); err != nil {
		return "", err
	}
	if response.ErrCode != 0 || response.AccessToken == "" {
		return "", fmt.Errorf("WeCom token exchange failed: code=%d message=%q", response.ErrCode, response.ErrMsg)
	}
	a.mu.Lock()
	a.tokens[connection.LoginProviderID] = cachedToken{value: response.AccessToken, expiresAt: time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)}
	a.mu.Unlock()
	return response.AccessToken, nil
}

func (a *Adapter) get(ctx context.Context, path string, values url.Values, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path+"?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WeCom request failed: status=%d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

var _ appdirectorysync.Connector = (*Adapter)(nil)
