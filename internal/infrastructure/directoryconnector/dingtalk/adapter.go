package dingtalk

import (
	"bytes"
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
type cachedToken struct {
	value     string
	expiresAt time.Time
}
type Adapter struct {
	resolver LoginProviderResolver
	client   *http.Client
	baseURL  string
	mu       sync.Mutex
	tokens   map[string]cachedToken
}

func NewAdapter(resolver LoginProviderResolver, client *http.Client, baseURL string) (*Adapter, error) {
	if resolver == nil {
		return nil, errors.New("DingTalk login provider resolver is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.dingtalk.com"
	}
	return &Adapter{resolver: resolver, client: client, baseURL: strings.TrimRight(baseURL, "/"), tokens: map[string]cachedToken{}}, nil
}
func (a *Adapter) Validate(ctx context.Context, c domain.Connection) (domain.Capabilities, error) {
	_, _, err := a.ListOrganizations(ctx, c)
	return domain.Capabilities{Organizations: true, People: true, Memberships: true, Events: true}, err
}
func (a *Adapter) ListOrganizations(ctx context.Context, c domain.Connection) ([]domain.Organization, string, error) {
	token, err := a.token(ctx, c)
	if err != nil {
		return nil, "", err
	}
	queue := []int64{1}
	seen := map[int64]bool{}
	result := []domain.Organization{}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		var response struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
			Result  []struct {
				DeptID   int64  `json:"dept_id"`
				ParentID int64  `json:"parent_id"`
				Name     string `json:"name"`
			} `json:"result"`
		}
		if err := a.post(ctx, "/topapi/v2/department/listsub", token, map[string]any{"dept_id": parent}, &response); err != nil {
			return nil, "", err
		}
		if response.ErrCode != 0 {
			return nil, "", fmt.Errorf("DingTalk department list failed: code=%d message=%q", response.ErrCode, response.ErrMsg)
		}
		for _, item := range response.Result {
			if seen[item.DeptID] {
				continue
			}
			seen[item.DeptID] = true
			parentID := fmt.Sprint(item.ParentID)
			if item.ParentID <= 1 {
				parentID = ""
			}
			result = append(result, domain.Organization{ConnectionID: c.ID, ExternalID: fmt.Sprint(item.DeptID), ExternalParentID: parentID, Name: item.Name, Status: domain.ProjectionActive})
			queue = append(queue, item.DeptID)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExternalID < result[j].ExternalID })
	return result, "", nil
}

type user struct {
	UserID  string  `json:"userid"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Mobile  string  `json:"mobile"`
	Avatar  string  `json:"avatar"`
	Active  bool    `json:"active"`
	DeptIDs []int64 `json:"dept_id_list"`
}

func (a *Adapter) allUsers(ctx context.Context, c domain.Connection) ([]user, error) {
	orgs, _, err := a.ListOrganizations(ctx, c)
	if err != nil {
		return nil, err
	}
	token, err := a.token(ctx, c)
	if err != nil {
		return nil, err
	}
	users := map[string]user{}
	for _, org := range orgs {
		deptID := int64(0)
		_, _ = fmt.Sscan(org.ExternalID, &deptID)
		cursor := int64(0)
		for {
			var response struct {
				ErrCode int    `json:"errcode"`
				ErrMsg  string `json:"errmsg"`
				Result  struct {
					HasMore    bool   `json:"has_more"`
					NextCursor int64  `json:"next_cursor"`
					List       []user `json:"list"`
				} `json:"result"`
			}
			if err := a.post(ctx, "/topapi/v2/user/list", token, map[string]any{"dept_id": deptID, "cursor": cursor, "size": 100}, &response); err != nil {
				return nil, err
			}
			if response.ErrCode != 0 {
				return nil, fmt.Errorf("DingTalk user list failed: code=%d message=%q", response.ErrCode, response.ErrMsg)
			}
			for _, item := range response.Result.List {
				users[item.UserID] = item
			}
			if !response.Result.HasMore {
				break
			}
			cursor = response.Result.NextCursor
		}
	}
	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]user, 0, len(keys))
	for _, key := range keys {
		result = append(result, users[key])
	}
	return result, nil
}
func (a *Adapter) ListPeople(ctx context.Context, c domain.Connection) ([]domain.Person, error) {
	users, err := a.allUsers(ctx, c)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Person, 0, len(users))
	for _, item := range users {
		status := domain.ProjectionActive
		if !item.Active {
			status = domain.ProjectionSuspended
		}
		result = append(result, domain.Person{ConnectionID: c.ID, ExternalID: item.UserID, ProviderSubject: item.UserID, Username: item.UserID, DisplayName: item.Name, Email: item.Email, Phone: item.Mobile, AvatarURL: item.Avatar, Status: status})
	}
	return result, nil
}
func (a *Adapter) ListMemberships(ctx context.Context, c domain.Connection) ([]domain.Membership, error) {
	users, err := a.allUsers(ctx, c)
	if err != nil {
		return nil, err
	}
	result := []domain.Membership{}
	for _, item := range users {
		for _, deptID := range item.DeptIDs {
			result = append(result, domain.Membership{ConnectionID: c.ID, ExternalPersonID: item.UserID, ExternalOrganizationID: fmt.Sprint(deptID), Status: domain.ProjectionActive})
		}
	}
	return result, nil
}
func (a *Adapter) token(ctx context.Context, c domain.Connection) (string, error) {
	a.mu.Lock()
	cached := a.tokens[c.LoginProviderID]
	if cached.value != "" && time.Until(cached.expiresAt) > time.Minute {
		a.mu.Unlock()
		return cached.value, nil
	}
	a.mu.Unlock()
	provider, err := a.resolver.ResolveLoginProvider(ctx, c.LoginProviderID)
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]string{"appKey": provider.ClientID, "appSecret": provider.ClientSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1.0/oauth2/accessToken", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.AccessToken == "" {
		return "", fmt.Errorf("DingTalk token exchange failed: status=%d", resp.StatusCode)
	}
	a.mu.Lock()
	a.tokens[c.LoginProviderID] = cachedToken{value: result.AccessToken, expiresAt: time.Now().Add(time.Duration(result.ExpireIn) * time.Second)}
	a.mu.Unlock()
	return result.AccessToken, nil
}
func (a *Adapter) post(ctx context.Context, path, token string, input any, output any) error {
	payload, _ := json.Marshal(input)
	endpoint := a.baseURL + path + "?" + url.Values{"access_token": {token}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DingTalk request failed: status=%d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

var _ appdirectorysync.Connector = (*Adapter)(nil)
