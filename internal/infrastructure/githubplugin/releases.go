package githubplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type releaseMetadata struct {
	Tag         string                 `json:"tag_name"`
	Draft       *bool                  `json:"draft"`
	Prerelease  *bool                  `json:"prerelease"`
	PublishedAt *time.Time             `json:"published_at"`
	Assets      []releaseAssetMetadata `json:"assets"`
}

func (release releaseMetadata) validPublished() bool {
	return release.Draft != nil && !*release.Draft && release.Prerelease != nil &&
		release.PublishedAt != nil && !release.PublishedAt.IsZero() && validRef(release.Tag)
}

func (client *Client) getPublishedReleaseTag(ctx context.Context, repository, tag string) (string, error) {
	release, err := client.getPublishedRelease(ctx, repository, tag)
	return release.Tag, err
}

func (client *Client) getPublishedRelease(ctx context.Context, repository, tag string) (releaseMetadata, error) {
	path := "/repos/" + repository + "/releases/latest"
	if tag != "" {
		path = "/repos/" + repository + "/releases/tags/" + url.PathEscape(tag)
	}
	response, err := client.get(ctx, githubRequest{
		host: apiHost, path: path, accept: apiJSON, maxBytes: maxRefPageBytes,
	})
	if err != nil {
		if tag == "" && response.status == http.StatusNotFound {
			return releaseMetadata{}, domainplugin.ErrRepositoryStableReleaseMissing
		}
		return releaseMetadata{}, err
	}
	var release releaseMetadata
	if err := json.Unmarshal(response.body, &release); err != nil {
		return releaseMetadata{}, invalidReleaseResponse()
	}
	if tag != "" && release.Draft != nil && *release.Draft {
		return releaseMetadata{}, apperrors.ErrNotFound
	}
	if !release.validPublished() || (tag == "" && *release.Prerelease) || (tag != "" && release.Tag != tag) {
		return releaseMetadata{}, invalidReleaseResponse()
	}
	return release, nil
}

func releaseVersion(tag string) string {
	version := strings.TrimPrefix(tag, "v")
	if _, err := semver.StrictNewVersion(version); err == nil {
		return version
	}
	return ""
}

func invalidReleaseResponse() error {
	return fmt.Errorf("%w: invalid github release response", apperrors.ErrServiceUnavailable)
}
