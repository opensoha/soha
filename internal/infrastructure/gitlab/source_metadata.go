package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var _ domainapp.SourceMetadataReader = (*Client)(nil)

func (c *Client) RepositoryCloneURLs(ctx context.Context, repositoryID string) ([]string, error) {
	data, err := c.readMetadata(ctx, "/projects/"+url.PathEscape(repositoryID), nil, 64<<10)
	if err != nil {
		return nil, err
	}
	var project struct {
		HTTPURL string `json:"http_url_to_repo"`
		SSHURL  string `json:"ssh_url_to_repo"`
	}
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("%w: invalid repository metadata", apperrors.ErrClusterUnready)
	}
	return []string{project.HTTPURL, project.SSHURL}, nil
}

func (c *Client) ResolveRepositoryRef(ctx context.Context, repositoryID, refType, ref string) (string, error) {
	resource := map[string]string{"branch": "branches", "tag": "tags", "commit": "commits"}[refType]
	if resource == "" {
		return "", fmt.Errorf("%w: unsupported reference type", apperrors.ErrInvalidArgument)
	}
	data, err := c.readMetadata(ctx, "/projects/"+url.PathEscape(repositoryID)+"/repository/"+resource+"/"+url.PathEscape(ref), nil, 256<<10)
	if err != nil {
		return "", err
	}
	var result struct {
		ID     string `json:"id"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("%w: invalid reference metadata", apperrors.ErrClusterUnready)
	}
	if refType == "commit" {
		return result.ID, nil
	}
	return result.Commit.ID, nil
}

func (c *Client) ReadRepositoryFile(ctx context.Context, repositoryID, commit, path string, limit int64) ([]byte, error) {
	return c.readMetadata(ctx, "/projects/"+url.PathEscape(repositoryID)+"/repository/files/"+url.PathEscape(path)+"/raw", url.Values{"ref": {commit}}, limit)
}

func (c *Client) readMetadata(ctx context.Context, path string, params url.Values, limit int64) ([]byte, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1<<20 {
		return nil, domainapp.ErrSourceFileTooLarge
	}
	req, err := c.request(ctx, path, params)
	if err != nil {
		return nil, err
	}
	// Metadata must stay on the explicitly configured provider endpoint. In
	// particular, PRIVATE-TOKEN must never follow a cross-host redirect.
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("%w: source provider request failed", apperrors.ErrClusterUnready)
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, apperrors.ErrAccessDenied
	case http.StatusNotFound:
		return nil, apperrors.ErrNotFound
	default:
		return nil, fmt.Errorf("%w: source provider status %d", apperrors.ErrClusterUnready, response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, domainapp.ErrSourceFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: source response interrupted", apperrors.ErrClusterUnready)
	}
	if int64(len(data)) > limit {
		return nil, domainapp.ErrSourceFileTooLarge
	}
	return data, nil
}

func (c *Client) request(ctx context.Context, path string, params url.Values) (*http.Request, error) {
	target := c.baseURL + path
	if encoded := params.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid source endpoint", apperrors.ErrInvalidArgument)
	}
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}
