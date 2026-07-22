package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (c *Client) TestConnection(ctx context.Context) error {
	if err := c.validateConfigured(); err != nil {
		return err
	}
	var response struct {
		ID int `json:"id"`
	}
	return c.get(ctx, "/user", nil, &response)
}

func (c *Client) ListRepositories(ctx context.Context, search, cursor string, limit int) ([]sohaapi.SourceRepository, string, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, "", err
	}
	page, err := parseCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = c.perPage
	}

	endpoint := "/projects"
	params := url.Values{}
	params.Set("simple", "true")
	params.Set("page", strconv.Itoa(page))
	params.Set("per_page", strconv.Itoa(limit))
	if search = strings.TrimSpace(search); search != "" {
		params.Set("search", search)
	}
	if c.groupID != "" {
		endpoint = "/groups/" + url.PathEscape(c.groupID) + "/projects"
		params.Del("simple")
	}

	var response []projectResponse
	headers, err := c.getWithHeaders(ctx, endpoint, params, &response)
	if err != nil {
		return nil, "", err
	}
	items := make([]sohaapi.SourceRepository, 0, len(response))
	for _, item := range response {
		items = append(items, sohaapi.SourceRepository{
			ID:            strconv.Itoa(item.ID),
			Name:          item.Name,
			FullName:      item.PathWithNamespace,
			Namespace:     item.Namespace.FullPath,
			WebURL:        item.WebURL,
			DefaultBranch: item.DefaultBranch,
			Archived:      item.Archived,
		})
	}
	return items, strings.TrimSpace(headers.Get("X-Next-Page")), nil
}

func (c *Client) ListRepositoryBranches(ctx context.Context, repositoryID, search string, limit int) ([]sohaapi.SourceBranch, error) {
	var response []branchResponse
	if err := c.listRepositoryReferences(ctx, repositoryID, search, limit, "branches", &response); err != nil {
		return nil, err
	}
	items := make([]sohaapi.SourceBranch, 0, len(response))
	for _, item := range response {
		items = append(items, sohaapi.SourceBranch{
			Name:          item.Name,
			CommitID:      item.Commit.ID,
			DefaultBranch: item.Default,
		})
	}
	return items, nil
}

func (c *Client) ListRepositoryTags(ctx context.Context, repositoryID, search string, limit int) ([]sohaapi.SourceTag, error) {
	var response []branchResponse
	if err := c.listRepositoryReferences(ctx, repositoryID, search, limit, "tags", &response); err != nil {
		return nil, err
	}
	items := make([]sohaapi.SourceTag, 0, len(response))
	for _, item := range response {
		items = append(items, sohaapi.SourceTag{Name: item.Name, CommitID: item.Commit.ID})
	}
	return items, nil
}

func (c *Client) listRepositoryReferences(ctx context.Context, repositoryID, search string, limit int, kind string, out *[]branchResponse) error {
	if err := c.validateConfigured(); err != nil {
		return err
	}
	if strings.TrimSpace(repositoryID) == "" {
		return fmt.Errorf("%w: gitlab repository id is required", apperrors.ErrInvalidArgument)
	}
	endpoint := "/projects/" + url.PathEscape(repositoryID) + "/repository/" + kind
	if limit < 1 || limit > 100 {
		limit = c.perPage
		if limit > 100 {
			limit = 100
		}
	}
	params := url.Values{}
	params.Set("per_page", strconv.Itoa(limit))
	if search = strings.TrimSpace(search); search != "" {
		params.Set("search", search)
	}
	return c.get(ctx, endpoint, params, out)
}

func (c *Client) GetRepositoryFile(ctx context.Context, repositoryID, ref, path string) (sohaapi.SourceFile, error) {
	if err := c.validateConfigured(); err != nil {
		return sohaapi.SourceFile{}, err
	}
	repositoryID = strings.TrimSpace(repositoryID)
	ref = strings.TrimSpace(ref)
	if repositoryID == "" || ref == "" || strings.TrimSpace(path) == "" {
		return sohaapi.SourceFile{}, fmt.Errorf("%w: gitlab repository id, ref, and path are required", apperrors.ErrInvalidArgument)
	}

	params := url.Values{}
	params.Set("ref", ref)
	var response fileResponse
	if err := c.get(ctx, "/projects/"+url.PathEscape(repositoryID)+"/repository/files/"+url.PathEscape(path), params, &response); err != nil {
		return sohaapi.SourceFile{}, err
	}
	if response.Encoding != sohaapi.SourceFileEncodingBase64 {
		return sohaapi.SourceFile{}, fmt.Errorf("unsupported gitlab file encoding %q", response.Encoding)
	}
	return sohaapi.SourceFile{
		RepositoryID: repositoryID,
		Path:         response.FilePath,
		Ref:          ref,
		BlobID:       response.BlobID,
		Encoding:     sohaapi.SourceFileEncoding(response.Encoding),
		Content:      response.Content,
		SizeBytes:    response.Size,
	}, nil
}

func parseCursor(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(cursor)
	if err != nil || page < 1 {
		return 0, fmt.Errorf("%w: gitlab repository cursor must be a positive page number", apperrors.ErrInvalidArgument)
	}
	return page, nil
}

type fileResponse struct {
	BlobID   string                     `json:"blob_id"`
	FilePath string                     `json:"file_path"`
	Encoding sohaapi.SourceFileEncoding `json:"encoding"`
	Content  string                     `json:"content"`
	Size     int64                      `json:"size"`
}
