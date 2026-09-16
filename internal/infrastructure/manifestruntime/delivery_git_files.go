package manifestruntime

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
)

func (r deliveryGitReader) files(ctx context.Context, commit string, source domaindocument.Source) ([]domaindocument.File, error) {
	if source.Path == "" || path.Clean(source.Path) != source.Path || path.IsAbs(source.Path) || source.Path == ".." || strings.HasPrefix(source.Path, "../") || strings.ContainsAny(source.Path, "\\:") || strings.ContainsFunc(source.Path, unicode.IsControl) {
		return nil, fmt.Errorf("Git source directory is invalid")
	}
	treeRef := commit + "^{tree}"
	if source.Path != "." {
		treeRef = commit + ":" + source.Path
	}
	objectType, err := r.output(ctx, 128, "cat-file", "-t", treeRef)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(objectType) != "tree" {
		return nil, fmt.Errorf("Git source directory is unavailable or is not a directory")
	}
	listing, err := r.output(ctx, gitMaxBytes, "ls-tree", "-r", "-z", treeRef)
	if err != nil {
		return nil, err
	}
	includes := source.IncludePatterns
	if len(includes) == 0 {
		includes = []string{"*.soha.yaml", "*.soha.yml", "*.soha.json", "**/*.soha.yaml", "**/*.soha.yml", "**/*.soha.json"}
	}
	files := make([]domaindocument.File, 0)
	total := 0
	for _, record := range strings.Split(listing, "\x00") {
		if record == "" {
			continue
		}
		name, object, err := selectedGitBlob(record, includes, source.ExcludePatterns)
		if err != nil {
			return nil, err
		}
		if object == "" {
			continue
		}
		if len(files) >= gitMaxFiles {
			return nil, fmt.Errorf("Git source exceeds the file count or byte limit")
		}
		content, err := r.blob(ctx, object, gitMaxBytes-total)
		if err != nil {
			return nil, err
		}
		total += len(content)
		files = append(files, domaindocument.File{Path: name, Content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (r deliveryGitReader) blob(ctx context.Context, object string, remaining int) (string, error) {
	value, err := r.output(ctx, 32, "cat-file", "-s", object)
	if err != nil {
		return "", err
	}
	size, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || size < 0 || size > 1<<20 || size > remaining {
		return "", fmt.Errorf("Git source exceeds the file count or byte limit")
	}
	content, err := r.output(ctx, 1<<20, "cat-file", "blob", object)
	if err != nil {
		return "", err
	}
	if len(content) != size || !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') {
		return "", fmt.Errorf("Git source document must be bounded UTF-8 text")
	}
	return content, nil
}

func selectedGitBlob(record string, includes, excludes []string) (string, string, error) {
	metadata, name, found := strings.Cut(record, "\t")
	fields := strings.Fields(metadata)
	if !found || len(fields) != 3 || !gitCommitPattern.MatchString(fields[2]) || name == "" || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", "", fmt.Errorf("Git source tree is invalid")
	}
	if fields[0] != "100644" && fields[0] != "100755" || fields[1] != "blob" {
		return "", "", fmt.Errorf("Git source directory contains a symbolic link, submodule or non-regular file")
	}
	if !manifestGlobSetMatches(name, includes) || manifestGlobSetMatches(name, excludes) {
		return "", "", nil
	}
	if ext := strings.ToLower(path.Ext(name)); ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return "", "", fmt.Errorf("Git source include pattern selected a non-YAML/JSON file")
	}
	return name, fields[2], nil
}
