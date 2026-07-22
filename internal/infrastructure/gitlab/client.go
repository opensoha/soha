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

type Options struct {
	BaseURL string
	Token   string
	GroupID string
	PerPage int
	Timeout time.Duration
	Enabled bool
}

func New(cfg cfgpkg.GitLabConfig) *Client {
	return NewWithOptions(Options{
		BaseURL: cfg.BaseURL,
		Token:   cfg.Token,
		GroupID: cfg.GroupID,
		PerPage: cfg.PerPage,
		Timeout: cfg.Timeout,
		Enabled: cfg.Enabled,
	})
}

func NewWithOptions(options Options) *Client {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	perPage := options.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(options.BaseURL), "/"),
		token:   strings.TrimSpace(options.Token),
		groupID: strings.TrimSpace(options.GroupID),
		perPage: perPage,
		http:    &http.Client{Timeout: timeout},
		enabled: options.Enabled,
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
	return c.listReferences(ctx, projectID, search, limit, "branches")
}

func (c *Client) ListTags(ctx context.Context, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	return c.listReferences(ctx, projectID, search, limit, "tags")
}

func (c *Client) ListCommits(ctx context.Context, projectID, search string, page, limit int) (domainapp.GitCommitPage, error) {
	if err := c.validateConfigured(); err != nil {
		return domainapp.GitCommitPage{}, err
	}
	if strings.TrimSpace(projectID) == "" {
		return domainapp.GitCommitPage{}, fmt.Errorf("%w: gitlab project id is required", apperrors.ErrInvalidArgument)
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = c.perPage
	}
	params := url.Values{
		"page":     []string{strconv.Itoa(page)},
		"per_page": []string{strconv.Itoa(limit + 1)},
	}
	if value := strings.TrimSpace(search); value != "" {
		params.Set("search", value)
	}
	var response []commitResponse
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectID)+"/repository/commits", params, &response); err != nil {
		return domainapp.GitCommitPage{}, err
	}
	hasMore := len(response) > limit
	if hasMore {
		response = response[:limit]
	}
	items := make([]domainapp.GitCommit, 0, len(response))
	for _, item := range response {
		items = append(items, domainapp.GitCommit{
			ID: item.ID, ShortID: item.ShortID, Title: item.Title, Message: item.Message,
			AuthorName: item.AuthorName, AuthorEmail: item.AuthorEmail, CommittedAt: item.CommittedDate, WebURL: item.WebURL,
		})
	}
	return domainapp.GitCommitPage{Items: items, Page: page, Limit: limit, HasMore: hasMore}, nil
}

func (c *Client) listReferences(ctx context.Context, projectID, search string, limit int, kind string) ([]domainapp.GitReference, error) {
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
	if err := c.get(ctx, "/projects/"+url.PathEscape(projectID)+"/repository/"+kind, params, &response); err != nil {
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
	_, err := c.getWithHeaders(ctx, path, params, out)
	return err
}

func (c *Client) getWithHeaders(ctx context.Context, path string, params url.Values, out any) (http.Header, error) {
	target := c.baseURL + path
	if encoded := params.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build gitlab request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("%w: gitlab request failed with status %d", apperrors.ErrClusterUnready, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("decode gitlab response: %w", err)
	}
	return resp.Header.Clone(), nil
}

type projectResponse struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
	Archived          bool   `json:"archived"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
}

type branchResponse struct {
	Name      string         `json:"name"`
	Protected bool           `json:"protected"`
	Default   bool           `json:"default"`
	Commit    commitResponse `json:"commit"`
}

type commitResponse struct {
	ID            string    `json:"id"`
	ShortID       string    `json:"short_id"`
	Title         string    `json:"title"`
	Message       string    `json:"message"`
	AuthorName    string    `json:"author_name"`
	AuthorEmail   string    `json:"author_email"`
	CommittedDate time.Time `json:"committed_date"`
	WebURL        string    `json:"web_url"`
}
