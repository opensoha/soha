package manifest

import (
	"fmt"
	"regexp"
	"strings"
)

type KustomizeImage struct {
	Name    string `json:"name"`
	NewName string `json:"newName,omitempty"`
	Digest  string `json:"digest"`
}

type KustomizeOptions struct {
	EntryPath string           `json:"entryPath,omitempty"`
	Images    []KustomizeImage `json:"images,omitempty"`
}

var (
	kustomizeEntryPattern  = regexp.MustCompile(`^(\.|\.?[A-Za-z0-9_-][A-Za-z0-9_.-]*(/\.?[A-Za-z0-9_-][A-Za-z0-9_.-]*)*)?$`)
	kustomizeImagePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	kustomizeDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func (options *KustomizeOptions) Validate() error {
	if options == nil {
		return nil
	}
	if len(options.EntryPath) > 1024 || !kustomizeEntryPattern.MatchString(options.EntryPath) {
		return fmt.Errorf("kustomize entryPath must be a directory within the package root")
	}
	if len(options.Images) > 50 {
		return fmt.Errorf("kustomize images cannot exceed 50 mappings")
	}
	seen := make(map[string]bool, len(options.Images))
	for _, image := range options.Images {
		if !kustomizeImagePattern.MatchString(image.Name) || seen[image.Name] {
			return fmt.Errorf("kustomize image names must be valid and unique")
		}
		seen[image.Name] = true
		if image.NewName != "" && (!kustomizeImagePattern.MatchString(image.NewName) || strings.LastIndex(image.NewName, ":") > strings.LastIndex(image.NewName, "/")) {
			return fmt.Errorf("kustomize newName must be an image repository without a tag or digest")
		}
		if !kustomizeDigestPattern.MatchString(image.Digest) {
			return fmt.Errorf("kustomize image mappings require a sha256 digest")
		}
	}
	return nil
}
