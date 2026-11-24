package githubplugin

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestMatchingReleasesRequiresExactTagCommit(t *testing.T) {
	t.Parallel()
	const version = "1.2.3"
	metadata := strings.TrimSuffix(testRelease, "}") +
		`,"target_commitish":"main","assets":[{"id":7,"name":"plugin.sig","size":3,"state":"uploaded"}]}`
	tests := []struct {
		name        string
		pinnedRef   string
		tagSHA      string
		fullRelease string
		tagStatus   int
		wantMatches int
		wantErr     error
	}{
		{"selected release", "refs/tags/v1.2.3", testCommit, metadata, http.StatusOK, 1, nil},
		{"matching branch", "refs/heads/main", testCommit, metadata, http.StatusOK, 1, nil},
		{"moved tag", "refs/tags/v1.2.3", strings.Repeat("a", 40), metadata, http.StatusOK, 0, nil},
		{"draft release", "refs/heads/main", "", strings.Replace(metadata, `"draft":false`, `"draft":true`, 1), http.StatusOK, 0, nil},
		{"first branch tag absent", "refs/heads/main", "", "", http.StatusNotFound, 0, nil},
		{"upstream failure", "refs/heads/main", "", "", http.StatusBadGateway, 0, apperrors.ErrServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
				switch request.URL.String() {
				case metadataURL:
					assertRequest(t, request, metadataURL, apiJSON, "Bearer "+testSecret)
					return stubResponse(http.StatusOK, testMetadata), nil
				case metadataURL + "/releases/tags/v" + version:
					assertRequest(t, request, request.URL.String(), apiJSON, "")
					return stubResponse(test.tagStatus, test.fullRelease), nil
				case metadataURL + "/releases/tags/" + version:
					if strings.HasPrefix(test.pinnedRef, "refs/tags/") {
						t.Fatal("looked up a different tag after an explicit release selection")
					}
					return stubResponse(http.StatusNotFound, ""), nil
				case metadataURL + "/commits/refs%2Ftags%2Fv" + version:
					assertRequest(t, request, request.URL.String(), apiSHA, "")
					return stubResponse(http.StatusOK, test.tagSHA), nil
				default:
					t.Fatal("unexpected GitHub endpoint")
					return nil, nil
				}
			})
			releases, err := client.MatchingReleases(t.Context(), domainplugin.PluginRepositoryPreviewSource{
				CanonicalSource: testSource, PinnedRef: test.pinnedRef, CommitSha: testCommit,
			}, version)
			if !errors.Is(err, test.wantErr) || len(releases) != test.wantMatches {
				t.Fatalf("MatchingReleases() returned %d matches and %v", len(releases), err)
			}
			if test.wantMatches == 1 {
				size, ok := releases[0].AssetSize("plugin.sig")
				if !ok || size != 3 {
					t.Fatalf("unexpected asset metadata: %d, %t", size, ok)
				}
			}
		})
	}
}

func TestCopyReleaseAssetRedirectAndBounds(t *testing.T) {
	t.Parallel()
	const cdn = "https://release-assets.githubusercontent.com/download?sig=opaque"
	release, err := newRelease("acme/plugin", []releaseAssetMetadata{{ID: 7, Name: "plugin.sig", Size: 3, State: "uploaded"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		redirect   string
		downstream string
		fetchCDN   bool
		wantErr    error
	}{
		{"direct byte stream", "", "", false, nil},
		{"supported CDN", cdn, "abc", true, nil},
		{"wrong scheme", "http://release-assets.githubusercontent.com/download", "", false, ErrEgressBlocked},
		{"wrong port", "https://release-assets.githubusercontent.com:8443/download", "", false, ErrEgressBlocked},
		{"user info", "https://user@release-assets.githubusercontent.com/download", "", false, ErrEgressBlocked},
		{"other host", "https://example.invalid/download", "", false, ErrEgressBlocked},
		{"oversized asset", cdn, "abcd", true, ErrResponseTooLarge},
		{"short asset", cdn, "ab", true, apperrors.ErrServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var calls int
			client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					assertRequest(t, request, metadataURL+"/releases/assets/7", "application/octet-stream", "")
					if test.redirect == "" {
						return stubResponse(http.StatusOK, "abc"), nil
					}
					response := stubResponse(http.StatusFound, "")
					response.Header = locationHeader(test.redirect)
					return response, nil
				}
				if calls != 2 || !test.fetchCDN {
					t.Fatal("unexpected redirect destination")
				}
				assertRequest(t, request, cdn, "application/octet-stream", "")
				return stubResponse(http.StatusOK, test.downstream), nil
			})
			var output bytes.Buffer
			err := client.CopyReleaseAsset(t.Context(), *release, "plugin.sig", 3, &output)
			if !errors.Is(err, test.wantErr) || (err == nil && output.String() != "abc") {
				t.Fatalf("CopyReleaseAsset() = %q, %v", output.String(), err)
			}
			if test.fetchCDN && calls != 2 {
				t.Fatalf("requests = %d, want API and CDN", calls)
			}
		})
	}
}
