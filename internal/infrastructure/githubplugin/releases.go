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
	Tag         string     `json:"tag_name"`
	Draft       *bool      `json:"draft"`
	Prerelease  *bool      `json:"prerelease"`
	PublishedAt *time.Time `json:"published_at"`
}

func (release releaseMetadata) validPublished() bool {
	return release.Draft != nil && !*release.Draft && release.Prerelease != nil &&
		release.PublishedAt != nil && !release.PublishedAt.IsZero() && validRef(release.Tag)
}

func (client *Client) getPublishedReleaseTag(ctx context.Context, repository, tag string) (string, error) {
	path := "/repos/" + repository + "/releases/latest"
	if tag != "" {
		path = "/repos/" + repository + "/releases/tags/" + url.PathEscape(tag)
	}
	response, err := client.get(ctx, githubRequest{
		host: apiHost, path: path, accept: apiJSON, maxBytes: maxRefPageBytes,
	})
	if err != nil {
		if tag == "" && response.status == http.StatusNotFound {
			return "", domainplugin.ErrRepositoryStableReleaseMissing
		}
		return "", err
	}
	var release releaseMetadata
	if err := json.Unmarshal(response.body, &release); err != nil {
		return "", invalidReleaseResponse()
	}
	if tag != "" && release.Draft != nil && *release.Draft {
		return "", apperrors.ErrNotFound
	}
	if !release.validPublished() || (tag == "" && *release.Prerelease) || (tag != "" && release.Tag != tag) {
		return "", invalidReleaseResponse()
	}
	return release.Tag, nil
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
