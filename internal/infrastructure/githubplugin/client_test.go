package githubplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	testCommit   = "abcdef0123456789abcdef0123456789abcdef01"
	testSource   = "https://github.com/acme/plugin"
	testMetadata = `{"full_name":"Acme/Plugin","private":false,"visibility":"public"}`
	testRelease  = `{"tag_name":"v1.2.3","draft":false,"prerelease":false,"published_at":"2026-11-20T08:00:00Z"}`
	testSecret   = "stub-credential-not-for-errors" // #nosec G101 -- Synthetic token sent only to stub HTTP transport to exercise redaction.
	testRemote   = "stub-upstream-not-for-errors"
	metadataURL  = "https://api.github.com/repos/acme/plugin"
	manifestURL  = "https://raw.githubusercontent.com/acme/plugin/" + testCommit + "/plugin.manifest.json"
)

func TestResolvePinsRevision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, source, ref, wantRef, revisionPath, body string
		releasePath, releaseBody                       string
	}{
		{
			name: "bare repository selects latest stable release", source: "https://github.com/Acme/Plugin.git/",
			releasePath: "/releases/latest", releaseBody: testRelease, wantRef: "refs/tags/v1.2.3",
			revisionPath: "/commits/refs%2Ftags%2Fv1.2.3", body: testCommit,
		},
		{
			name: "release URL uses exact published tag", source: testSource + "/releases/tag/pre/view",
			releasePath: "/releases/tags/pre%2Fview", wantRef: "refs/tags/pre/view",
			releaseBody:  `{"tag_name":"pre/view","draft":false,"prerelease":true,"published_at":"2026-11-20T08:00:00Z"}`,
			revisionPath: "/commits/refs%2Ftags%2Fpre%2Fview", body: testCommit,
		},
		{
			name: "slash in tree branch", source: testSource + "/tree/release/Stable/", wantRef: "release/Stable",
			revisionPath: "/commits/release%2FStable", body: testCommit + "\n",
		},
		{
			name: "explicit tag", source: testSource, ref: "v1.2.3", wantRef: "v1.2.3",
			revisionPath: "/commits/v1.2.3", body: testCommit,
		},
		{
			name: "explicit branch namespace stays qualified", source: testSource,
			ref: "refs/heads/v1.2.3", wantRef: "refs/heads/v1.2.3",
			revisionPath: "/commits/refs%2Fheads%2Fv1.2.3", body: testCommit,
		},
		{
			name: "full SHA is still verified by GitHub", source: testSource, ref: strings.ToUpper(testCommit),
			wantRef: testCommit, revisionPath: "/commits/" + testCommit, body: testCommit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var calls int
			expectedCalls := 2
			if test.releasePath != "" {
				expectedCalls++
			}
			client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
				calls++
				switch {
				case calls == 1:
					assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
					return stubResponse(http.StatusOK, testMetadata), nil
				case calls == 2 && test.releasePath != "":
					assertRequest(t, request, metadataURL+test.releasePath, apiJSON, "")
					return stubResponse(http.StatusOK, test.releaseBody), nil
				case calls == expectedCalls:
					assertRequest(t, request, metadataURL+test.revisionPath, apiSHA, "")
					return stubResponse(http.StatusOK, test.body), nil
				default:
					t.Fatal("unexpected extra HTTP request")
					return nil, nil
				}
			})
			source, ref, sha, err := client.Resolve(t.Context(), test.source, test.ref)
			if err != nil || source != testSource || ref != test.wantRef || sha != testCommit || calls != expectedCalls {
				t.Fatalf("Resolve() = %q, %q, %q, %v; requests = %d", source, ref, sha, err, calls)
			}
		})
	}
}

func TestResolveRejectsUnverifiedRevision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, ref, body string
	}{
		{name: "short SHA from upstream", ref: "main", body: "abcdef0"},
		{name: "unexpected SHA from upstream", ref: testCommit, body: strings.Repeat("c", 40)},
		{name: "uppercase SHA from upstream", ref: "main", body: strings.ToUpper(testCommit)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := stubClient("", func(request *http.Request) (*http.Response, error) {
				if request.URL.String() == metadataURL {
					assertRequest(t, request, metadataURL, apiJSON, "")
					return stubResponse(http.StatusOK, testMetadata), nil
				}
				return stubResponse(http.StatusOK, test.body), nil
			})
			source, ref, sha, err := client.Resolve(t.Context(), testSource, test.ref)
			if !errors.Is(err, apperrors.ErrServiceUnavailable) || source != "" || ref != "" || sha != "" {
				t.Fatalf("Resolve() = %q, %q, %q, %v; want only upstream error", source, ref, sha, err)
			}
		})
	}
}

func TestResolveReleaseDoesNotFallBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, suffix, requestPath, body string
		status                          int
		want                            error
	}{
		{name: "no stable release", status: http.StatusNotFound, want: domainplugin.ErrRepositoryStableReleaseMissing},
		{name: "conflict is not a missing stable release", status: http.StatusConflict, want: apperrors.ErrNotFound},
		{name: "latest rate limited", status: http.StatusTooManyRequests, want: ErrRateLimited},
		{name: "latest service failure", status: http.StatusBadGateway, want: apperrors.ErrServiceUnavailable},
		{name: "latest is a prerelease", status: http.StatusOK, body: strings.Replace(testRelease, `"prerelease":false`, `"prerelease":true`, 1), want: apperrors.ErrServiceUnavailable},
		{name: "missing publication timestamp", status: http.StatusOK, body: `{"tag_name":"v1.2.3","draft":false,"prerelease":false}`, want: apperrors.ErrServiceUnavailable},
		{name: "tag without release", suffix: "/releases/tag/v2.0.0", requestPath: "/releases/tags/v2.0.0", status: http.StatusNotFound, want: apperrors.ErrNotFound},
		{name: "tag mismatch", suffix: "/releases/tag/v2.0.0", requestPath: "/releases/tags/v2.0.0", status: http.StatusOK, body: testRelease, want: apperrors.ErrServiceUnavailable},
		{name: "explicit draft", suffix: "/releases/tag/v1.2.3", requestPath: "/releases/tags/v1.2.3", status: http.StatusOK, body: strings.Replace(testRelease, `"draft":false`, `"draft":true`, 1), want: apperrors.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := test.requestPath
			if path == "" {
				path = "/releases/latest"
			}
			client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
				if request.URL.String() == metadataURL {
					return stubResponse(http.StatusOK, testMetadata), nil
				}
				assertRequest(t, request, metadataURL+path, apiJSON, "")
				return stubResponse(test.status, test.body), nil
			})
			source, ref, sha, err := client.Resolve(t.Context(), testSource+test.suffix, "")
			assertSafeError(t, err, test.want)
			if test.want != domainplugin.ErrRepositoryStableReleaseMissing && errors.Is(err, domainplugin.ErrRepositoryStableReleaseMissing) {
				t.Fatal("an upstream failure was reported as a clean absence of stable releases")
			}
			if source != "" || ref != "" || sha != "" {
				t.Fatalf("failed resolution returned %q, %q, %q", source, ref, sha)
			}
		})
	}
}

func TestRejectsNonpublicOrMismatchedMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		want       error
	}{
		{name: "private", body: `{"private":true,"visibility":"private"}`, want: apperrors.ErrNotFound},
		{name: "internal", body: `{"private":false,"visibility":"internal"}`, want: apperrors.ErrNotFound},
		{name: "missing private marker", body: `{"visibility":"public"}`, want: apperrors.ErrNotFound},
		{name: "missing visibility", body: `{"private":false}`, want: apperrors.ErrNotFound},
		{
			name: "different repository", body: strings.Replace(testMetadata, "Acme/Plugin", "Other/Plugin", 1),
			want: apperrors.ErrServiceUnavailable,
		},
		{name: "malformed metadata", body: testRemote, want: apperrors.ErrServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
				assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
				return stubResponse(http.StatusOK, test.body), nil
			})
			_, _, _, resolveErr := client.Resolve(t.Context(), testSource, "main")
			assertSafeError(t, resolveErr, test.want)
			body, fetchErr := client.FetchManifest(t.Context(), testSource, testCommit)
			assertSafeError(t, fetchErr, test.want)
			if body != nil {
				t.Fatal("FetchManifest returned bytes without confirming the public repository")
			}
		})
	}
}

func TestResolveDoesNotDistinguishPrivateFromMissingRepository(t *testing.T) {
	t.Parallel()
	private := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
		assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
		return stubResponse(http.StatusOK, `{"private":true,"visibility":"private"}`), nil
	})
	missing := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
		assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
		return stubResponse(http.StatusNotFound, testRemote), nil
	})
	_, _, _, privateErr := private.Resolve(t.Context(), testSource, "main")
	_, _, _, missingErr := missing.Resolve(t.Context(), testSource, "main")
	assertSafeError(t, privateErr, apperrors.ErrNotFound)
	assertSafeError(t, missingErr, apperrors.ErrNotFound)
	if privateErr.Error() != missingErr.Error() {
		t.Fatal("private and missing repository errors must not differ")
	}
}

func TestFetchManifestReadsOnlyPinnedRootBytes(t *testing.T) {
	t.Parallel()
	const raw = "{\n  \"preserve\": \"these exact bytes\"\n}\n"
	var calls int
	client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
			return stubResponse(http.StatusOK, testMetadata), nil
		}
		assertRequest(t, request, manifestURL, "*/*", "")
		return stubResponse(http.StatusOK, raw), nil
	})
	body, err := client.FetchManifest(t.Context(), testSource, testCommit)
	if err != nil || string(body) != raw || calls != 2 {
		t.Fatalf("FetchManifest() = %q, %v; requests = %d", body, err, calls)
	}
}

func TestFetchManifestRejectsUnpinnedOrNoncanonicalInput(t *testing.T) {
	t.Parallel()
	client := stubClient("", func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input reached the HTTP transport")
		return nil, nil
	})
	for _, test := range []struct{ source, sha string }{
		{source: testSource + "/tree/main", sha: testCommit},
		{source: testSource + ".git", sha: testCommit},
		{source: testSource, sha: "main"},
		{source: testSource, sha: testCommit[:12]},
		{source: testSource, sha: strings.ToUpper(testCommit)},
	} {
		body, err := client.FetchManifest(t.Context(), test.source, test.sha)
		if !errors.Is(err, apperrors.ErrInvalidArgument) || body != nil {
			t.Errorf("FetchManifest(%q, %q) = %q, %v; want only invalid argument", test.source, test.sha, body, err)
		}
	}
}

func TestFetchManifestResponseBoundsAndStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		header http.Header
		body   string
		length int64
		want   error
	}{
		{name: "manifest missing", status: http.StatusNotFound, want: apperrors.ErrNotFound},
		{name: "redirect to fixed host", status: http.StatusMovedPermanently, header: locationHeader(manifestURL), want: ErrEgressBlocked},
		{
			name: "redirect to local address", status: http.StatusFound,
			header: locationHeader("https://127.0.0.1/" + testSecret), want: ErrEgressBlocked,
		},
		{name: "429", status: http.StatusTooManyRequests, want: ErrRateLimited},
		{
			name: "403 with exhausted quota", status: http.StatusForbidden,
			header: http.Header{"X-Ratelimit-Remaining": {"0"}}, want: ErrRateLimited,
		},
		{name: "403 without rate limit", status: http.StatusForbidden, want: apperrors.ErrServiceUnavailable},
		{
			name: "partial content", status: http.StatusPartialContent, want: apperrors.ErrServiceUnavailable,
		},
		{
			name: "unexpected compression", status: http.StatusOK, header: http.Header{"Content-Encoding": {"gzip"}},
			want: apperrors.ErrServiceUnavailable,
		},
		{name: "content length above limit", status: http.StatusOK, length: maxManifestBytes + 1, want: ErrResponseTooLarge},
		{
			name: "unknown length above limit", status: http.StatusOK, length: -1,
			body: strings.Repeat("x", maxManifestBytes+1), want: ErrResponseTooLarge,
		},
		{name: "exact byte limit", status: http.StatusOK, length: -1, body: strings.Repeat("x", maxManifestBytes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var calls int
			client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
					return stubResponse(http.StatusOK, testMetadata), nil
				}
				if calls > 2 {
					t.Fatal("unexpected HTTP request after raw response")
				}
				assertRequest(t, request, manifestURL, "*/*", "")
				body := test.body
				if body == "" {
					body = testRemote + testSecret
				}
				response := stubResponse(test.status, body)
				response.Header = test.header
				response.ContentLength = test.length
				return response, nil
			})
			body, err := client.FetchManifest(t.Context(), testSource, testCommit)
			if test.want == nil {
				if err != nil || string(body) != test.body {
					t.Fatalf("FetchManifest() returned %d bytes and %v; want exact limit and no error", len(body), err)
				}
				return
			}
			assertSafeError(t, err, test.want)
			if body != nil {
				t.Fatal("FetchManifest returned bytes after rejecting the upstream response")
			}
		})
	}
}

func TestResolveSanitizesTransportErrors(t *testing.T) {
	t.Parallel()
	client := stubClient(testSecret, func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("%s %s", testSecret, testRemote)
	})
	_, _, _, err := client.Resolve(t.Context(), testSource, "main")
	assertSafeError(t, err, apperrors.ErrServiceUnavailable)
}

func TestResolveCancellationAndClientTimeout(t *testing.T) {
	t.Parallel()
	t.Run("canceled by caller", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
			cancel(errors.New(testSecret))
			return nil, context.Cause(request.Context())
		})
		_, _, _, err := client.Resolve(ctx, testSource, "main")
		assertSafeError(t, err, context.Canceled)
	})
	t.Run("bounded by client timeout", func(t *testing.T) {
		t.Parallel()
		client := stubClient("", func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		client.httpClient.Timeout = time.Millisecond
		_, _, _, err := client.Resolve(t.Context(), testSource, "main")
		assertSafeError(t, err, context.DeadlineExceeded)
		assertSafeError(t, err, apperrors.ErrServiceUnavailable)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func stubClient(token string, transport roundTripFunc) *Client {
	client := New(token)
	client.httpClient.Transport = transport
	return client
}

func stubResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
	}
}

func locationHeader(location string) http.Header {
	return http.Header{"Location": {location}}
}

func assertRequest(t *testing.T, request *http.Request, target, accept, authorization string) {
	t.Helper()
	if request.Method != http.MethodGet || request.URL.String() != target || request.Header.Get("Accept") != accept {
		t.Fatalf("unexpected HTTP request: %s %s", request.Method, request.URL)
	}
	if request.Header.Get("Accept-Encoding") != "identity" {
		t.Fatal("expected a request for unencoded bytes")
	}
	if request.Header.Get("Authorization") != authorization {
		t.Fatal("authorization header not scoped to repository metadata")
	}
}

func assertSafeError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v; want %v", err, want)
	}
	if strings.Contains(err.Error(), testSecret) || strings.Contains(err.Error(), testRemote) {
		t.Fatal("error exposes unsanitized upstream data")
	}
}
