package githubplugin

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestParseSource(t *testing.T) {
	t.Parallel()
	const root = "https://github.com/acme/plugin"
	tests := []struct {
		name          string
		repositoryURL string
		ref           string
		wantRef       string
		wantRelease   bool
	}{
		{name: "implicit default", repositoryURL: root},
		{name: "canonical case and git suffix", repositoryURL: " https://GitHub.com/Acme/Plugin.GIT/ "},
		{name: "tag", repositoryURL: root, ref: "v1.2.3", wantRef: "v1.2.3"},
		{name: "tree branch", repositoryURL: root + "/tree/release/Stable/", wantRef: "release/Stable"},
		{
			name: "escaped branch agrees with explicit ref", repositoryURL: root + "/tree/release%2FStable",
			ref: "release/Stable", wantRef: "release/Stable",
		},
		{
			name: "full commit normalizes hex case", repositoryURL: root + "/tree/" + testCommit,
			ref: strings.ToUpper(testCommit), wantRef: testCommit,
		},
		{
			name: "release with escaped slash", repositoryURL: root + "/releases/tag/release%2FStable",
			ref: "release/Stable", wantRef: "release/Stable", wantRelease: true,
		},
		{
			name: "release tag preserves commit-like case", repositoryURL: root + "/releases/tag/" + strings.ToUpper(testCommit),
			wantRef: strings.ToUpper(testCommit), wantRelease: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source, err := parseSource(test.repositoryURL, test.ref)
			if err != nil || canonicalRepositoryURL(source.repository) != root || source.ref != test.wantRef || source.release != test.wantRelease {
				t.Fatalf("parseSource() = %+v, %v; want ref %q, release %t", source, err, test.wantRef, test.wantRelease)
			}
		})
	}
}

func TestParseSourceRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	const root = "https://github.com/acme/plugin"
	tests := []struct {
		name          string
		repositoryURL string
		ref           string
	}{
		{name: "insecure scheme", repositoryURL: "http://github.com/acme/plugin"},
		{name: "foreign host", repositoryURL: "https://example.invalid/acme/plugin"},
		{name: "noncanonical port", repositoryURL: "https://github.com:443/acme/plugin"},
		{name: "credentials", repositoryURL: "https://user:test-secret@github.com/acme/plugin"}, // #nosec G101 -- Synthetic URL credentials must be rejected before HTTP.
		{name: "query", repositoryURL: root + "?tab=readme"},
		{name: "fragment", repositoryURL: root + "#"},
		{name: "missing repository", repositoryURL: "https://github.com/acme"},
		{name: "escaped repository segment", repositoryURL: "https://github.com/acme%2Fother/plugin"},
		{name: "dot repository", repositoryURL: "https://github.com/acme/.."},
		{name: "double trailing slash", repositoryURL: root + "//"},
		{name: "blob link", repositoryURL: root + "/blob/main/plugin.manifest.json"},
		{name: "issue link", repositoryURL: root + "/issues/1"},
		{name: "missing tree ref", repositoryURL: root + "/tree/"},
		{name: "missing release tag", repositoryURL: root + "/releases/tag/"},
		{name: "unsupported release path", repositoryURL: root + "/releases/latest"},
		{name: "conflicting release ref", repositoryURL: root + "/releases/tag/v1.0.0", ref: "v2.0.0"},
		{name: "escaping invalid release tag", repositoryURL: root + "/releases/tag/%2E%2E/main"},
		{name: "conflicting refs", repositoryURL: root + "/tree/main", ref: "Main"},
		{name: "escaping invalid ref", repositoryURL: root + "/tree/%2E%2E/main"},
		{name: "double escaped ref", repositoryURL: root + "/tree/release%252Fstable"},
		{name: "git expression", repositoryURL: root, ref: "main~1"},
		{name: "ref contains blank", repositoryURL: root, ref: "main branch"},
		{name: "ref exceeds limit", repositoryURL: root, ref: strings.Repeat("a", maxRefLen+1)},
		{name: "URL exceeds limit", repositoryURL: root + strings.Repeat("a", maxRepositoryURLLen)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := stubClient("", func(_ *http.Request) (*http.Response, error) {
				t.Fatal("invalid input reached the HTTP transport")
				return nil, nil
			})
			source, ref, sha, err := client.Resolve(t.Context(), test.repositoryURL, test.ref)
			if !errors.Is(err, apperrors.ErrInvalidArgument) || source != "" || ref != "" || sha != "" {
				t.Fatalf("Resolve() = %q, %q, %q, %v; want only an invalid argument error", source, ref, sha, err)
			}
			if strings.Contains(err.Error(), "test-secret") {
				t.Fatal("input credentials leaked in validation error")
			}
		})
	}
}
