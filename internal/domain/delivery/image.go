package delivery

import (
	"fmt"
	"regexp"
	"strings"
)

var imageSHA256Pattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var imageNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*$`)

// ImmutableImageReference binds a registry image to the reported content digest.
// A tag may identify the build output, but is never retained for deployment.
func ImmutableImageReference(ref, digest string) (string, error) {
	ref, digest = strings.TrimSpace(ref), strings.TrimSpace(digest)
	if !imageSHA256Pattern.MatchString(digest) {
		return "", fmt.Errorf("a complete sha256 image digest is required")
	}
	name, embedded, pinned := strings.Cut(ref, "@")
	if pinned && embedded != digest {
		return "", fmt.Errorf("image reference and digest disagree")
	}
	if !imageNamePattern.MatchString(name) || strings.Contains(name, "//") {
		return "", fmt.Errorf("a registry image reference is required")
	}
	if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
		name = name[:colon]
	}
	if name == "" || strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("an image repository is required")
	}
	return name + "@" + digest, nil
}
