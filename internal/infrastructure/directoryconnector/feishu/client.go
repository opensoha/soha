package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	directoryconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector"
)

const defaultBaseURL = "https://open.feishu.cn"

type Client struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
	pageSize    int
}

type Option func(*Client) error

func WithBaseURL(value string) Option {
	return func(c *Client) error {
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return errors.New("invalid Feishu base URL")
		}
		c.baseURL = strings.TrimRight(value, "/")
		return nil
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) error {
		if client == nil {
			return errors.New("HTTP client is required")
		}
		c.httpClient = client
		return nil
	}
}

func WithPageSize(size int) Option {
	return func(c *Client) error {
		if size < 1 || size > 50 {
			return errors.New("Feishu page size must be between 1 and 50")
		}
		c.pageSize = size
		return nil
	}
}

func New(accessToken string, options ...Option) (*Client, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("Feishu access token is required")
	}
	c := &Client{baseURL: defaultBaseURL, accessToken: accessToken, httpClient: &http.Client{Timeout: 15 * time.Second}, pageSize: 50}
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) Provider() string { return "feishu" }

func (c *Client) Capabilities() directoryconnector.Capabilities {
	return directoryconnector.Capabilities{Organizations: true, People: true, Memberships: true, Events: true}
}

func (c *Client) Validate(ctx context.Context) error {
	_, err := c.ListOrganizations(ctx, "")
	return err
}

func (c *Client) ListOrganizations(ctx context.Context, pageToken string) (directoryconnector.Page[directoryconnector.Organization], error) {
	values := url.Values{"parent_department_id": {"0"}, "department_id_type": {"open_department_id"}, "fetch_child": {"true"}, "page_size": {strconv.Itoa(c.pageSize)}}
	setPageToken(values, pageToken)
	var data struct {
		Items []struct {
			DepartmentID       string `json:"department_id"`
			OpenDepartmentID   string `json:"open_department_id"`
			Name               string `json:"name"`
			ParentDepartmentID string `json:"parent_department_id"`
			LeaderUserID       string `json:"leader_user_id"`
		} `json:"items"`
		PageToken string `json:"page_token"`
	}
	if err := c.get(ctx, "list organizations", "/open-apis/contact/v3/departments", values, &data); err != nil {
		return directoryconnector.Page[directoryconnector.Organization]{}, err
	}
	items := make([]directoryconnector.Organization, 0, len(data.Items))
	for _, item := range data.Items {
		id := firstNonEmpty(item.OpenDepartmentID, item.DepartmentID)
		if id == "" || item.Name == "" {
			continue
		}
		parent := item.ParentDepartmentID
		if parent == "0" {
			parent = ""
		}
		items = append(items, directoryconnector.Organization{ExternalID: id, Name: item.Name, ParentExternalID: parent})
	}
	return directoryconnector.Page[directoryconnector.Organization]{Items: items, NextToken: data.PageToken}, nil
}

func (c *Client) ListPeople(ctx context.Context, organizationID, pageToken string) (directoryconnector.Page[directoryconnector.Person], error) {
	data, err := c.listUsers(ctx, organizationID, pageToken)
	if err != nil {
		return directoryconnector.Page[directoryconnector.Person]{}, err
	}
	items := make([]directoryconnector.Person, 0, len(data.Items))
	for _, item := range data.Items {
		id := firstNonEmpty(item.OpenID, item.UserID)
		if id == "" {
			continue
		}
		items = append(items, directoryconnector.Person{
			ExternalID: id, ProviderSubject: item.OpenID, UnionID: item.UnionID, Name: item.Name,
			Email: item.Email, Mobile: item.Mobile, AvatarURL: firstNonEmpty(item.Avatar.AvatarOrigin, item.Avatar.Avatar640), Active: item.Status.IsActivated && !item.Status.IsFrozen && !item.Status.IsResigned,
		})
	}
	return directoryconnector.Page[directoryconnector.Person]{Items: items, NextToken: data.PageToken}, nil
}

func (c *Client) ListMemberships(ctx context.Context, organizationID, pageToken string) (directoryconnector.Page[directoryconnector.Membership], error) {
	data, err := c.listUsers(ctx, organizationID, pageToken)
	if err != nil {
		return directoryconnector.Page[directoryconnector.Membership]{}, err
	}
	items := make([]directoryconnector.Membership, 0)
	for _, item := range data.Items {
		personID := firstNonEmpty(item.OpenID, item.UserID)
		for _, departmentID := range item.DepartmentIDs {
			if personID != "" && departmentID != "" {
				items = append(items, directoryconnector.Membership{PersonExternalID: personID, OrganizationExternalID: departmentID})
			}
		}
	}
	return directoryconnector.Page[directoryconnector.Membership]{Items: items, NextToken: data.PageToken}, nil
}

type userPage struct {
	Items []struct {
		UserID        string   `json:"user_id"`
		OpenID        string   `json:"open_id"`
		UnionID       string   `json:"union_id"`
		Name          string   `json:"name"`
		Email         string   `json:"email"`
		Mobile        string   `json:"mobile"`
		DepartmentIDs []string `json:"department_ids"`
		Avatar        struct {
			AvatarOrigin string `json:"avatar_origin"`
			Avatar640    string `json:"avatar_640"`
		} `json:"avatar"`
		Status struct {
			IsFrozen    bool `json:"is_frozen"`
			IsResigned  bool `json:"is_resigned"`
			IsActivated bool `json:"is_activated"`
		} `json:"status"`
	} `json:"items"`
	PageToken string `json:"page_token"`
}

func (c *Client) listUsers(ctx context.Context, organizationID, pageToken string) (userPage, error) {
	if organizationID == "" {
		return userPage{}, errors.New("Feishu organization ID is required to list people")
	}
	values := url.Values{"department_id": {organizationID}, "department_id_type": {"open_department_id"}, "user_id_type": {"open_id"}, "page_size": {strconv.Itoa(c.pageSize)}}
	setPageToken(values, pageToken)
	var data userPage
	err := c.get(ctx, "list people", "/open-apis/contact/v3/users/find_by_department", values, &data)
	return data, err
}

func (c *Client) get(ctx context.Context, operation, path string, values url.Values, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+values.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build Feishu request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Feishu %s: %w", operation, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read Feishu %s response: %w", operation, err)
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decodeErr := json.Unmarshal(body, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil || envelope.Code != 0 {
		message := envelope.Msg
		if decodeErr != nil {
			message = "invalid JSON response"
		}
		return &directoryconnector.ProviderError{Provider: "feishu", Operation: operation, StatusCode: resp.StatusCode, Code: envelope.Code, RequestID: requestID(resp.Header), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()), Message: message}
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("decode Feishu %s data: %w", operation, err)
	}
	return nil
}

func setPageToken(values url.Values, token string) {
	if token != "" {
		values.Set("page_token", token)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func requestID(header http.Header) string {
	return firstNonEmpty(header.Get("X-Tt-Logid"), header.Get("X-Request-Id"))
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

var _ directoryconnector.Connector = (*Client)(nil)
