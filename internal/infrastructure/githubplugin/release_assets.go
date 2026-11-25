package githubplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const releaseDownloadTimeout = 2 * time.Minute

// ErrAssetLengthMismatch means the downloaded bytes disagree with the release metadata.
var ErrAssetLengthMismatch = fmt.Errorf("%w: github asset length differs from published metadata", apperrors.ErrServiceUnavailable)

type releaseAssetMetadata struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	State string `json:"state"`
}

// Release identifies a published release whose tag resolved to the selected commit.
type Release struct {
	repository string
	assets     map[string]releaseAssetMetadata
}

// AssetSize returns the published byte length of an attached asset.
func (release Release) AssetSize(name string) (int64, bool) {
	asset, ok := release.assets[name]
	return asset.Size, ok
}

// MatchingReleases checks selected releases directly. For other refs it checks the
// conventional version tags, which must independently resolve to the same commit.
func (client *Client) MatchingReleases(
	ctx context.Context, source domainplugin.PluginRepositoryPreviewSource, version string,
) ([]Release, error) {
	parsed, err := parseSource(source.CanonicalSource, "")
	if err != nil || canonicalRepositoryURL(parsed.repository) != source.CanonicalSource {
		return nil, invalidRepositoryURL()
	}
	if !commitPattern.MatchString(source.CommitSha) || !validRef(source.PinnedRef) {
		return nil, fmt.Errorf("%w: invalid pinned github release identity", apperrors.ErrInvalidArgument)
	}
	if err := client.checkPublicRepository(ctx, parsed.repository); err != nil {
		return nil, err
	}
	tags := []string{"v" + version, version}
	tag, releaseRef := strings.CutPrefix(source.PinnedRef, "refs/tags/")
	if releaseRef {
		tags = []string{tag}
	}
	matches := make([]Release, 0, len(tags))
	for _, tag := range tags {
		if !validRef(tag) {
			continue
		}
		release, err := client.matchRelease(ctx, parsed.repository, tag, source.CommitSha)
		if err != nil {
			return nil, err
		}
		if release != nil {
			matches = append(matches, *release)
		}
	}
	return matches, nil
}

func (client *Client) matchRelease(ctx context.Context, repository, tag, commit string) (*Release, error) {
	metadata, err := client.getPublishedRelease(ctx, repository, tag)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read matching github release: %w", err)
	}
	// target_commitish is mutable or can name a branch. Resolve the actual tag.
	resolved, err := client.resolveRef(ctx, repository, "refs/tags/"+tag)
	if err != nil {
		return nil, fmt.Errorf("resolve matching github release tag: %w", err)
	}
	if resolved != commit {
		return nil, nil
	}
	return newRelease(repository, metadata.Assets)
}

func newRelease(repository string, assets []releaseAssetMetadata) (*Release, error) {
	if assets == nil {
		return nil, invalidReleaseResponse()
	}
	release := &Release{repository: repository, assets: make(map[string]releaseAssetMetadata, len(assets))}
	names := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if _, duplicate := names[asset.Name]; duplicate || asset.ID <= 0 || asset.Name == "" || asset.Size < 0 {
			return nil, invalidReleaseResponse()
		}
		names[asset.Name] = struct{}{}
		if asset.State == "uploaded" {
			release.assets[asset.Name] = asset
		}
	}
	return release, nil
}

// CopyReleaseAsset downloads an exact release asset ID, exchanging no credentials
// with either the public asset API or its narrowly allowlisted CDN.
func (client *Client) CopyReleaseAsset(
	ctx context.Context, release Release, name string, limit int64, destination io.Writer,
) error {
	asset, ok := release.assets[name]
	if !ok {
		return apperrors.ErrNotFound
	}
	if asset.Size > limit {
		return ErrResponseTooLarge
	}
	ctx, cancel := context.WithTimeout(ctx, releaseDownloadTimeout)
	defer cancel()
	location := fmt.Sprintf("https://%s/repos/%s/releases/assets/%d", apiHost, release.repository, asset.ID)
	response, err := client.openReleaseAsset(ctx, location, true)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return copyReleaseResponse(ctx, response, asset.Size, destination)
}

func (client *Client) openReleaseAsset(ctx context.Context, location string, fromAPI bool) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, ErrEgressBlocked
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Soha-GitHub-Plugin")
	if fromAPI {
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	// Reuse the hardened transport. The enclosing context bounds streaming time.
	streamClient := &http.Client{Transport: client.httpClient.Transport, CheckRedirect: client.httpClient.CheckRedirect}
	response, err := streamClient.Do(request)
	if err != nil {
		return nil, requestFailure(ctx, err)
	}
	if fromAPI && response.StatusCode == http.StatusFound {
		location, err := response.Location()
		_ = response.Body.Close()
		if err != nil || location.Scheme != "https" || !isReleaseAssetHost(location.Host) ||
			location.User != nil || location.Opaque != "" || location.Fragment != "" {
			return nil, ErrEgressBlocked
		}
		return client.openReleaseAsset(ctx, location.String(), false)
	}
	if err := responseStatusError(response); err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	return response, nil
}

func copyReleaseResponse(ctx context.Context, response *http.Response, size int64, destination io.Writer) error {
	if len(response.Header.Values("Content-Encoding")) != 0 || response.Uncompressed {
		return fmt.Errorf("%w: unexpected github asset encoding", apperrors.ErrServiceUnavailable)
	}
	if response.ContentLength > size {
		return ErrResponseTooLarge
	}
	written, err := io.Copy(destination, io.LimitReader(response.Body, size+1))
	if err != nil {
		return requestFailure(ctx, err)
	}
	if written != size {
		return ErrAssetLengthMismatch
	}
	return nil
}
