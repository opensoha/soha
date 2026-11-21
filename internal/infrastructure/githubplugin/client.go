// Package githubplugin resolves public GitHub revisions and reads pinned plugin manifests.
package githubplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	manifestFilename = "plugin.manifest.json"
	maxManifestBytes = 2 << 20 // Match the existing marketplace manifest limit.
	maxMetadataBytes = 256 << 10
	maxSHABytes      = 128
	apiJSON          = "application/vnd.github+json"
	apiSHA           = "application/vnd.github.sha"
)

var (
	// ErrEgressBlocked means DNS/Dial policy or the no-redirect policy rejected the request.
	ErrEgressBlocked = fmt.Errorf("%w: github destination is not allowed", apperrors.ErrServiceUnavailable)
	// ErrRateLimited means GitHub returned an explicit rate-limit response.
	ErrRateLimited = domainplugin.ErrRepositoryRateLimited
	// ErrResponseTooLarge means GitHub exceeded the response byte limit for the operation.
	ErrResponseTooLarge = fmt.Errorf("%w: github response exceeds size limit", apperrors.ErrServiceUnavailable)
)

// Client reads only public repositories. Construct it with New; it is safe for concurrent use.
type Client struct {
	httpClient      *http.Client
	publicReadToken string
	refPages        refPageCache
}

// New constructs a direct-HTTPS client. The optional token is sent only for repository
// metadata; revision and raw-content reads never authenticate.
func New(publicReadToken string) *Client {
	return &Client{httpClient: newHTTPClient(), publicReadToken: strings.TrimSpace(publicReadToken)}
}

// Resolve returns the canonical repository URL, selected revision, and the full lowercase
// SHA of its commit. A bare repository selects the latest stable published release.
// Resolved releases use a fully qualified tag.
func (client *Client) Resolve(
	ctx context.Context,
	repositoryURL, ref string,
) (canonicalSource, pinnedRef, commitSHA string, err error) {
	source, err := parseSource(repositoryURL, ref)
	if err != nil {
		return "", "", "", err
	}
	if err := client.checkPublicRepository(ctx, source.repository); err != nil {
		return "", "", "", err
	}
	if source.release || source.ref == "" {
		source.ref, err = client.getPublishedReleaseTag(ctx, source.repository, source.ref)
		if err != nil {
			return "", "", "", fmt.Errorf("resolve github release: %w", err)
		}
		source.release = true
	}
	revision := source.ref
	if source.release {
		revision = "refs/tags/" + revision
	}
	commitSHA, err = client.resolveRef(ctx, source.repository, revision)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve github revision: %w", err)
	}
	return canonicalRepositoryURL(source.repository), revision, commitSHA, nil
}

// FetchManifest returns uninterpreted root-manifest bytes at a full lowercase commit SHA.
// It rechecks repository visibility without resolving any mutable revision.
func (client *Client) FetchManifest(ctx context.Context, canonicalSource, commitSHA string) ([]byte, error) {
	source, err := parseSource(canonicalSource, "")
	if err != nil || canonicalSource != canonicalRepositoryURL(source.repository) {
		return nil, invalidRepositoryURL()
	}
	if !commitPattern.MatchString(commitSHA) {
		return nil, fmt.Errorf("%w: expected a full lowercase github commit sha", apperrors.ErrInvalidArgument)
	}
	if err := client.checkPublicRepository(ctx, source.repository); err != nil {
		return nil, err
	}
	path := "/" + source.repository + "/" + commitSHA + "/" + manifestFilename
	body, err := client.getBytes(ctx, githubRequest{
		host: rawHost, path: path, accept: "*/*", maxBytes: maxManifestBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch github plugin manifest: %w", err)
	}
	return body, nil
}

type repositoryMetadata struct {
	FullName   string `json:"full_name"`
	Private    *bool  `json:"private"`
	Visibility string `json:"visibility"`
}

func (client *Client) checkPublicRepository(ctx context.Context, repository string) error {
	body, err := client.getBytes(ctx, githubRequest{
		host: apiHost, path: "/repos/" + repository, accept: apiJSON,
		maxBytes: maxMetadataBytes, token: client.publicReadToken,
	})
	if err != nil {
		return fmt.Errorf("read github repository metadata: %w", err)
	}
	var metadata repositoryMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return fmt.Errorf("%w: invalid github repository metadata", apperrors.ErrServiceUnavailable)
	}
	// Do not confirm that a private repository exists, even if the token can see it.
	if metadata.Private == nil || *metadata.Private || metadata.Visibility != "public" {
		return fmt.Errorf("read github repository metadata: %w", apperrors.ErrNotFound)
	}
	if strings.ToLower(metadata.FullName) != repository {
		return fmt.Errorf("%w: unexpected github repository identity", apperrors.ErrServiceUnavailable)
	}
	return nil
}

func (client *Client) resolveRef(ctx context.Context, repository, ref string) (string, error) {
	// Request the SHA representation rather than a commit object that may carry a large diff.
	body, err := client.getBytes(ctx, githubRequest{
		host: apiHost, path: "/repos/" + repository + "/commits/" + url.PathEscape(ref),
		accept: apiSHA, maxBytes: maxSHABytes,
	})
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(body))
	if !commitPattern.MatchString(sha) || (commitPattern.MatchString(ref) && sha != ref) {
		return "", fmt.Errorf("%w: invalid github commit response", apperrors.ErrServiceUnavailable)
	}
	return sha, nil
}

type githubRequest struct {
	host     string
	path     string
	accept   string
	maxBytes int64
	token    string
	etag     string
}

type githubResponse struct {
	status int
	header http.Header
	body   []byte
}

func (client *Client) getBytes(ctx context.Context, target githubRequest) ([]byte, error) {
	response, err := client.get(ctx, target)
	return response.body, err
}

func (client *Client) get(ctx context.Context, target githubRequest) (githubResponse, error) {
	if err := ctx.Err(); err != nil {
		return githubResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+target.host+target.path, nil)
	if err != nil {
		return githubResponse{}, requestFailure(ctx, err)
	}
	request.Header.Set("User-Agent", "Soha-GitHub-Plugin")
	request.Header.Set("Accept", target.accept)
	request.Header.Set("Accept-Encoding", "identity")
	if target.etag != "" {
		request.Header.Set("If-None-Match", target.etag)
	}
	if target.host == apiHost {
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if target.token != "" {
			request.Header.Set("Authorization", "Bearer "+target.token)
		}
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return githubResponse{}, requestFailure(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()
	result := githubResponse{status: response.StatusCode, header: response.Header}
	if result.status == http.StatusNotModified && target.etag != "" {
		return result, nil
	}
	if err := responseStatusError(response); err != nil {
		if errors.Is(err, ErrRateLimited) {
			err = &rateLimitError{until: rateLimitUntil(time.Now(), response.Header)}
		}
		return result, err
	}
	result.body, err = readBody(ctx, response, target.maxBytes)
	return result, err
}

func responseStatusError(response *http.Response) error {
	status := response.StatusCode
	switch {
	case status == http.StatusOK:
		return nil
	case status >= 300 && status < 400:
		return ErrEgressBlocked
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status == http.StatusForbidden && rateLimitHeaders(response.Header):
		return ErrRateLimited
	case status == http.StatusNotFound || status == http.StatusConflict:
		return apperrors.ErrNotFound
	default:
		return fmt.Errorf("%w: unexpected github response status", apperrors.ErrServiceUnavailable)
	}
}

func rateLimitHeaders(headers http.Header) bool {
	return headers.Get("X-RateLimit-Remaining") == "0" || headers.Get("Retry-After") != ""
}

func readBody(ctx context.Context, response *http.Response, limit int64) ([]byte, error) {
	if response.ContentLength > limit {
		return nil, ErrResponseTooLarge
	}
	if len(response.Header.Values("Content-Encoding")) != 0 || response.Uncompressed {
		return nil, fmt.Errorf("%w: unexpected github response encoding", apperrors.ErrServiceUnavailable)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, requestFailure(ctx, err)
	}
	if int64(len(body)) > limit {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}

func requestFailure(ctx context.Context, err error) error {
	// Keep transport/parse errors out of the chain: they can contain upstream or request data.
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, ErrEgressBlocked):
		return ErrEgressBlocked
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", apperrors.ErrServiceUnavailable, context.DeadlineExceeded)
	default:
		return fmt.Errorf("%w: github request failed", apperrors.ErrServiceUnavailable)
	}
}
