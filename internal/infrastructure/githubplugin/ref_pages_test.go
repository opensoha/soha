package githubplugin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	releasePage1 = "/releases?per_page=20&page=1"
	releasePage2 = "/releases?per_page=20&page=2"
	branchPage1  = "/branches?per_page=20&page=1"
)

func TestListRepositoryRefsRequestsOnlySelectedPage(t *testing.T) {
	t.Parallel()
	var metadataCalls, releaseCalls, branchCalls int
	client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case metadataURL:
			metadataCalls++
			if request.Header.Get("Authorization") != "Bearer "+testSecret {
				t.Error("metadata request is missing configured token")
			}
			return stubResponse(http.StatusOK, testMetadata), nil
		case metadataURL + releasePage2:
			releaseCalls++
			if request.Header.Get("Authorization") != "" {
				t.Error("release page request exposed the metadata token")
			}
			preview := strings.ReplaceAll(testRelease, "v1.2.3", "1.5.0-preview.1+build.1")
			preview = strings.Replace(preview, `"prerelease":false`, `"prerelease":true`, 1)
			response := stubResponse(http.StatusOK, `[`+testRelease+`,`+preview+`,{"draft":true},`+
				strings.Replace(testRelease, "v1.2.3", "special/update", 1)+`]`)
			response.Header.Set("Link", `<http://127.0.0.1/private>; rel="next", <https://api.github.com>; rel="last"`)
			return response, nil
		case metadataURL + branchPage1:
			branchCalls++
			return stubResponse(http.StatusOK, `[{"name":"main"},{"name":"release/stable"}]`), nil
		default:
			t.Errorf("unexpected upstream request for %s", request.URL)
			return stubResponse(http.StatusBadRequest, ""), nil
		}
	})
	result, err := client.ListReleases(t.Context(), testSource, 2)
	if err != nil || len(result.Refs) != 3 || !result.HasMore {
		t.Fatalf("release page = %+v, %v", result, err)
	}
	first := result.Refs[0]
	if first.Name != "v1.2.3" || first.Version != "1.2.3" || first.Prerelease || first.PublishedAt == nil || first.ReleaseURL != testSource+"/releases/tag/v1.2.3" {
		t.Fatalf("release version = %+v", first)
	}
	if result.Refs[1].Version != "1.5.0-preview.1+build.1" || !result.Refs[1].Prerelease ||
		result.Refs[2].Version != "" || result.Refs[2].ReleaseURL != testSource+"/releases/tag/special%2Fupdate" {
		t.Fatalf("prerelease or non-SemVer tag = %+v", result.Refs[1:])
	}
	result.Refs[0].Name = "modified by caller"
	*result.Refs[0].PublishedAt = time.Time{}
	result, err = client.ListReleases(t.Context(), "https://GitHub.com/Acme/Plugin.git/", 2)
	if err != nil || result.Refs[0].Name != "v1.2.3" || result.Refs[0].PublishedAt.IsZero() {
		t.Fatalf("cached release page = %+v, %v", result, err)
	}
	branches, err := client.ListBranches(t.Context(), testSource, 1)
	if err != nil || len(branches.Refs) != 2 || branches.Refs[1].Name != "release/stable" || branches.HasMore {
		t.Fatalf("branch page = %+v, %v", branches, err)
	}
	if metadataCalls != 2 || releaseCalls != 1 || branchCalls != 1 {
		t.Fatalf("request counts = %d metadata, %d releases, %d branches", metadataCalls, releaseCalls, branchCalls)
	}
}

func TestRefPageRevalidationAndBackoffNeverServeStaleResults(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls int
		const etag = `"release-list-v1"`
		client := stubClient("", func(request *http.Request) (*http.Response, error) {
			if request.URL.String() == metadataURL {
				return stubResponse(http.StatusOK, testMetadata), nil
			}
			if request.URL.String() != metadataURL+releasePage1 {
				t.Errorf("unexpected upstream page request for %s", request.URL)
			}
			calls++
			if calls == 1 {
				response := stubResponse(http.StatusOK, `[`+testRelease+`]`)
				response.Header.Set("ETag", etag)
				response.Header.Set("Link", `<https://api.github.com/repos/acme/plugin/releases?page=2>; rel="next"`)
				return response, nil
			}
			if request.Header.Get("If-None-Match") != etag {
				t.Error("refresh request did not send cached ETag")
			}
			switch calls {
			case 2:
				return stubResponse(http.StatusNotModified, ""), nil
			case 3:
				return stubResponse(http.StatusBadGateway, testRemote), nil
			case 4:
				response := stubResponse(http.StatusTooManyRequests, testRemote)
				response.Header.Set("Retry-After", "60")
				return response, nil
			default:
				return stubResponse(http.StatusOK, `[]`), nil
			}
		})
		for range 2 {
			result, err := client.ListReleases(t.Context(), testSource, 1)
			if err != nil || len(result.Refs) != 1 || !result.HasMore {
				t.Fatalf("release page after revalidation = %+v, %v", result, err)
			}
			time.Sleep(refCacheTTL)
		}
		for range 2 {
			result, err := client.ListReleases(t.Context(), testSource, 1)
			assertSafeError(t, err, apperrors.ErrServiceUnavailable)
			if len(result.Refs) != 0 || result.HasMore || calls != 3 {
				t.Fatalf("failed refresh = %+v, %d HTTP page requests", result, calls)
			}
		}
		time.Sleep(refErrorBackoff)
		for range 2 {
			result, err := client.ListReleases(t.Context(), testSource, 1)
			assertSafeError(t, err, ErrRateLimited)
			if len(result.Refs) != 0 || result.HasMore || calls != 4 {
				t.Fatalf("rate-limited page = %+v, %d HTTP page requests", result, calls)
			}
			time.Sleep(30 * time.Second)
		}
		result, err := client.ListReleases(t.Context(), testSource, 1)
		if err != nil || result.Refs == nil || len(result.Refs) != 0 || result.HasMore || calls != 5 {
			t.Fatalf("recovered empty page = %+v, %v; requests %d", result, err, calls)
		}
	})
}

func TestRefPageSharesFlightAcrossCallers(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls int
		started, finish := make(chan struct{}), make(chan struct{})
		client := stubClient("", func(request *http.Request) (*http.Response, error) {
			if request.URL.String() == metadataURL {
				return stubResponse(http.StatusOK, testMetadata), nil
			}
			calls++
			if calls == 1 {
				close(started)
			}
			<-finish
			return stubResponse(http.StatusOK, `[{"name":"main"}]`), nil
		})
		first, cancel := context.WithCancel(t.Context())
		defer cancel()
		firstErr := make(chan error, 1)
		go func() {
			_, err := client.ListBranches(first, testSource, 1)
			firstErr <- err
		}()
		<-started
		secondErr := make(chan error, 1)
		go func() {
			_, err := client.ListBranches(t.Context(), testSource, 1)
			secondErr <- err
		}()
		synctest.Wait()
		cancel()
		assertSafeError(t, <-firstErr, context.Canceled)
		close(finish)
		if err := <-secondErr; err != nil || calls != 1 {
			t.Fatalf("shared refresh = %v, %d HTTP page requests", err, calls)
		}
	})
}

func TestRefPagesRejectInvalidInputAndNonpublicRepositories(t *testing.T) {
	t.Parallel()
	client := stubClient(testSecret, func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != metadataURL {
			t.Errorf("nonpublic repository reached the list endpoint %s", request.URL)
		}
		return stubResponse(http.StatusOK, `{"private":true,"visibility":"private"}`), nil
	})
	for _, page := range []int{0, -1, maxRefPage + 1} {
		_, err := client.ListBranches(t.Context(), testSource, page)
		assertSafeError(t, err, apperrors.ErrInvalidArgument)
	}
	_, err := client.ListBranches(t.Context(), "https://127.0.0.1/acme/plugin", 1)
	assertSafeError(t, err, apperrors.ErrInvalidArgument)
	_, err = client.ListBranches(t.Context(), testSource, 1)
	assertSafeError(t, err, apperrors.ErrNotFound)
}
