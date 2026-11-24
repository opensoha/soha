package githubplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	refPageSize     = 20
	maxRefPage      = 1000
	maxRefPageBytes = 2 << 20
)

// ListReleases returns a page of published GitHub releases, including prereleases.
func (client *Client) ListReleases(ctx context.Context, repositoryURL string, page int) (domainplugin.PluginRepositoryRefList, error) {
	return client.listRefs(ctx, repositoryURL, page, "releases", releaseRefs)
}

// ListBranches returns a page of public GitHub branches.
func (client *Client) ListBranches(ctx context.Context, repositoryURL string, page int) (domainplugin.PluginRepositoryRefList, error) {
	return client.listRefs(ctx, repositoryURL, page, "branches", branchRefs)
}

func (client *Client) listRefs(
	ctx context.Context, repositoryURL string, page int, endpoint string,
	decode func(string, []byte) ([]domainplugin.PluginRepositoryRef, error),
) (domainplugin.PluginRepositoryRefList, error) {
	source, err := parseSource(repositoryURL, "")
	if err != nil {
		return domainplugin.PluginRepositoryRefList{}, err
	}
	if page < 1 || page > maxRefPage {
		return domainplugin.PluginRepositoryRefList{}, fmt.Errorf("%w: github repository page must be between 1 and %d", apperrors.ErrInvalidArgument, maxRefPage)
	}
	path := fmt.Sprintf("/repos/%s/%s?per_page=%d&page=%d", source.repository, endpoint, refPageSize, page)
	return client.refPages.load(ctx, path, func(ctx context.Context, previous cachedRefPage) (cachedRefPage, error) {
		if err := client.checkPublicRepository(ctx, source.repository); err != nil {
			return cachedRefPage{}, err
		}
		response, err := client.get(ctx, githubRequest{
			host: apiHost, path: path, accept: apiJSON, maxBytes: maxRefPageBytes, etag: previous.etag,
		})
		if err != nil {
			return cachedRefPage{}, fmt.Errorf("list github %s: %w", endpoint, err)
		}
		if response.status == http.StatusNotModified {
			if len(response.header.Values("Link")) > 0 {
				previous.list.HasMore = page < maxRefPage && hasNextPage(response.header)
			}
			return previous, nil
		}
		refs, err := decode(source.repository, response.body)
		if err != nil {
			return cachedRefPage{}, err
		}
		return cachedRefPage{
			list: domainplugin.PluginRepositoryRefList{
				Refs: refs, HasMore: page < maxRefPage && hasNextPage(response.header),
			},
			etag: response.header.Get("ETag"),
		}, nil
	})
}

func releaseRefs(repository string, body []byte) ([]domainplugin.PluginRepositoryRef, error) {
	var releases []releaseMetadata
	if err := json.Unmarshal(body, &releases); err != nil || releases == nil || len(releases) > refPageSize {
		return nil, invalidReleaseResponse()
	}
	refs := make([]domainplugin.PluginRepositoryRef, 0, len(releases))
	for _, release := range releases {
		if release.Draft != nil && *release.Draft {
			continue
		}
		if !release.validPublished() {
			return nil, invalidReleaseResponse()
		}
		refs = append(refs, domainplugin.PluginRepositoryRef{
			Name: release.Tag, Version: releaseVersion(release.Tag), PublishedAt: release.PublishedAt,
			Prerelease: *release.Prerelease,
			ReleaseURL: canonicalRepositoryURL(repository) + "/releases/tag/" + url.PathEscape(release.Tag),
		})
	}
	return refs, nil
}

func branchRefs(_ string, body []byte) ([]domainplugin.PluginRepositoryRef, error) {
	var branches []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &branches); err != nil || branches == nil || len(branches) > refPageSize {
		return nil, invalidBranchResponse()
	}
	refs := make([]domainplugin.PluginRepositoryRef, 0, len(branches))
	for _, branch := range branches {
		if !validRef(branch.Name) {
			return nil, invalidBranchResponse()
		}
		refs = append(refs, domainplugin.PluginRepositoryRef{Name: branch.Name})
	}
	return refs, nil
}

func invalidBranchResponse() error {
	return fmt.Errorf("%w: invalid github branches response", apperrors.ErrServiceUnavailable)
}

func hasNextPage(header http.Header) bool {
	for _, links := range header.Values("Link") {
		for link := range strings.SplitSeq(links, ",") {
			_, attributes, ok := strings.Cut(link, ">")
			if ok && strings.Contains(attributes, `; rel="next"`) {
				return true
			}
		}
	}
	return false
}
