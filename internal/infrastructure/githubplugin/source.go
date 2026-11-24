package githubplugin

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	repositoryHost      = "github.com"
	maxRepositoryURLLen = 2048
	maxRefLen           = 512
)

var (
	ownerPattern      = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)
	repositoryPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)
	commitPattern     = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type repositorySource struct {
	repository string
	ref        string
	release    bool
}

func parseSource(repositoryURL, explicitRef string) (repositorySource, error) {
	if len(repositoryURL) > maxRepositoryURLLen || strings.ContainsAny(repositoryURL, "?#") {
		return repositorySource{}, invalidRepositoryURL()
	}
	parsed, err := url.Parse(strings.TrimSpace(repositoryURL))
	if err != nil || !isRepositoryURL(parsed) {
		return repositorySource{}, invalidRepositoryURL()
	}
	path := strings.TrimSuffix(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return repositorySource{}, invalidRepositoryURL()
	}
	owner := strings.ToLower(parts[0])
	name := strings.TrimSuffix(strings.ToLower(parts[1]), ".git")
	if !validRepositoryName(owner, name) {
		return repositorySource{}, invalidRepositoryURL()
	}
	var suffix string
	if len(parts) == 3 {
		if parts[2] == "" {
			return repositorySource{}, invalidRepositoryURL()
		}
		suffix = parts[2]
	}
	ref, release, err := selectRef(suffix, explicitRef)
	if err != nil {
		return repositorySource{}, err
	}
	return repositorySource{repository: owner + "/" + name, ref: ref, release: release}, nil
}

func isRepositoryURL(parsed *url.URL) bool {
	return parsed.Scheme == "https" && strings.EqualFold(parsed.Host, repositoryHost) &&
		parsed.User == nil && parsed.Opaque == ""
}

func validRepositoryName(owner, name string) bool {
	return ownerPattern.MatchString(owner) && !strings.Contains(owner, "--") &&
		repositoryPattern.MatchString(name) && name != "." && name != ".." && !strings.HasSuffix(name, ".git")
}

func selectRef(suffix, explicitRef string) (ref string, release bool, err error) {
	if explicitRef != "" && !validRef(explicitRef) {
		return "", false, fmt.Errorf("%w: invalid github revision", apperrors.ErrInvalidArgument)
	}
	if suffix == "" {
		return normalizeCommitRef(explicitRef), false, nil
	}
	escapedRef, release := strings.CutPrefix(suffix, "releases/tag/")
	if !release {
		var tree bool
		escapedRef, tree = strings.CutPrefix(suffix, "tree/")
		if !tree {
			return "", false, invalidRepositoryURL()
		}
	}
	ref, err = url.PathUnescape(escapedRef)
	if err != nil || !validRef(ref) {
		return "", false, fmt.Errorf("%w: invalid github url revision", apperrors.ErrInvalidArgument)
	}
	if !release {
		ref, explicitRef = normalizeCommitRef(ref), normalizeCommitRef(explicitRef)
	}
	if explicitRef != "" && explicitRef != ref {
		return "", false, fmt.Errorf("%w: conflicting github revisions", apperrors.ErrInvalidArgument)
	}
	return ref, release, nil
}

func validRef(ref string) bool {
	if ref == "" || len(ref) > maxRefLen || !utf8.ValidString(ref) {
		return false
	}
	if ref == "@" || strings.HasPrefix(ref, "-") || strings.HasSuffix(ref, ".") {
		return false
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.ContainsAny(ref, "~^:?*[\\%#") {
		return false
	}
	if strings.ContainsFunc(ref, func(char rune) bool { return unicode.IsSpace(char) || unicode.IsControl(char) }) {
		return false
	}
	for component := range strings.SplitSeq(ref, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func normalizeCommitRef(ref string) string {
	lower := strings.ToLower(ref)
	if commitPattern.MatchString(lower) {
		return lower
	}
	return ref
}

func canonicalRepositoryURL(repository string) string {
	return "https://" + repositoryHost + "/" + repository
}

func invalidRepositoryURL() error {
	return fmt.Errorf("%w: expected a github https repository, tree, or release url", apperrors.ErrInvalidArgument)
}
