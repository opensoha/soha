package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Client struct {
	baseURL string
	token   string
	groupID string
	perPage int
	http    *http.Client
	enabled bool
}

func New(cfg cfgpkg.GitLabConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	perPage := cfg.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		token:   strings.TrimSpace(cfg.Token),
		groupID: strings.TrimSpace(cfg.GroupID),
		perPage: perPage,
		http:    &http.Client{Timeout: timeout},
		enabled: cfg.Enabled,
	}
}

func (c *Client) ListProjects(ctx context.Context, search string, limit int) ([]domainapp.GitRepository, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = c.perPage
	}
	endpoint := "/projects"
	params := url.Values{}
	params.Set("simple", "true")
	params.Set("per_page", strconv.Itoa(limit))
	if strings.TrimSpace(search) != "" {
		params.Set("search", strings.TrimSpace(search))
	}
	if c.groupID != "" {
		endpoint = "/groups/" + url.PathEscape(c.groupID) + "/projects"
		params.Del("simple")
	}
	var response []projectResponse
	if err := c.get(ctx, endpoint, params, &response); err != nil {
		return nil, err
	}
	items := make([]domainapp.GitRepository, 0, len(response))
	for _, item := range response {
		items = append(items, domainapp.GitRepository{
			ID:                strconv.Itoa(item.ID),
			Name:              item.Name,
			Path:              item.Path,
			PathWithNamespace: item.PathWithNamespace,
			DefaultBranch:     item.DefaultBranch,
			WebURL:            item.WebURL,
		})
	}
	return items, nil
}

func (c *Client) ListBranches(ctx context.Context, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: gitlab project id is required", apperrors.ErrInvalidArgument)
	}
	if limit <= 0 {
		limit = c.perPage
	}
	params := url.Values{}
	params.Set("per_page", strconv.Itoa(limit))
	if strings.TrimSpace(search) != "" {
		params.Set("search", strings.TrimSpace(search))
	}
	var response []branchResponse
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectID)+"/repository/branches", params, &response); err != nil {
		return nil, err
	}
	items := make([]domainapp.GitReference, 0, len(response))
	for _, item := range response {
		items = append(items, domainapp.GitReference{
			Name:      item.Name,
			CommitSHA: item.Commit.ID,
			Protected: item.Protected,
			UpdatedAt: item.Commit.CommittedDate,
		})
	}
	return items, nil
}

func (c *Client) ListTags(ctx context.Context, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("%w: gitlab project id is required", apperrors.ErrInvalidArgument)
	}
	if limit <= 0 {
		limit = c.perPage
	}
	params := url.Values{}
	params.Set("per_page", strconv.Itoa(limit))
	if strings.TrimSpace(search) != "" {
		params.Set("search", strings.TrimSpace(search))
	}
	var response []tagResponse
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectID)+"/repository/tags", params, &response); err != nil {
		return nil, err
	}
	items := make([]domainapp.GitReference, 0, len(response))
	for _, item := range response {
		items = append(items, domainapp.GitReference{
			Name:      item.Name,
			CommitSHA: item.Commit.ID,
			Protected: item.Protected,
			UpdatedAt: item.Commit.CommittedDate,
		})
	}
	return items, nil
}

func (c *Client) validateConfigured() error {
	if !c.enabled {
		return fmt.Errorf("%w: gitlab integration is disabled", apperrors.ErrAccessDenied)
	}
	if c.baseURL == "" || c.token == "" {
		return fmt.Errorf("%w: gitlab integration is not configured", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	target := c.baseURL + path
	if encoded := params.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build gitlab request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%w: gitlab request failed with status %d", apperrors.ErrClusterUnready, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode gitlab response: %w", err)
	}
	return nil
}

type projectResponse struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
}

type branchResponse struct {
	Name      string         `json:"name"`
	Protected bool           `json:"protected"`
	Commit    commitResponse `json:"commit"`
}

type tagResponse struct {
	Name      string         `json:"name"`
	Protected bool           `json:"protected"`
	Commit    commitResponse `json:"commit"`
}

type commitResponse struct {
	ID            string    `json:"id"`
	CommittedDate time.Time `json:"committed_date"`
}
