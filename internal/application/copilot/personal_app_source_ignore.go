package copilot

import (
	"archive/tar"
	"errors"
	"io"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/opensoha/soha/contracts/apphosting/sourceignore"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxWorkspaceIgnoreBytes      = 2 << 20
	maxWorkspaceIgnoreCandidates = 1 << 14
)

type workspaceIgnoreFile struct {
	offset  int64
	size    int64
	invalid bool
}

func readWorkspaceSourceIgnores(raw io.ReadSeeker, sourcePath string) (*sourceignore.Rules, error) {
	files, err := workspaceIgnoreFiles(raw, sourcePath)
	if err != nil {
		return nil, err
	}

	var rules sourceignore.Rules
	remaining := int64(maxWorkspaceIgnoreBytes)
	for _, directory := range slices.Sorted(maps.Keys(files)) {
		// A child rule file cannot affect an excluded parent, regardless of tar order.
		if rules.Ignored(directory, true) {
			continue
		}
		entry := files[directory]
		if entry.invalid || entry.size < 0 || entry.size > min(remaining, sourceignore.MaxFileBytes) {
			return nil, invalidWorkspaceIgnoreFile()
		}
		if _, err := raw.Seek(entry.offset, io.SeekStart); err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		data := make([]byte, entry.size)
		if _, err := io.ReadFull(raw, data); err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		if err := rules.Add(directory, data); err != nil {
			return nil, invalidWorkspaceIgnoreFile()
		}
		remaining -= entry.size
	}
	if _, err := raw.Seek(0, io.SeekStart); err != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	return &rules, nil
}

func workspaceIgnoreFiles(raw io.ReadSeeker, sourcePath string) (map[string]workspaceIgnoreFile, error) {
	files := make(map[string]workspaceIgnoreFile)
	reader := tar.NewReader(raw)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		entryPath, isRoot, err := sanitizeExportTarPath(header.Name, header.Typeflag == tar.TypeDir)
		if err != nil {
			return nil, err
		}
		rel, inScope, _ := matchWorkspaceSubpath(entryPath, sourcePath)
		if isRoot || !inScope || path.Base(rel) != ".gitignore" || skipDynamicWorkspaceNoise(rel) {
			continue
		}
		directory := path.Dir(rel)
		if directory == "." {
			directory = ""
		}
		if _, exists := files[directory]; exists || len(files) >= maxWorkspaceIgnoreCandidates {
			if !exists {
				return nil, invalidWorkspaceIgnoreFile()
			}
			files[directory] = workspaceIgnoreFile{invalid: true}
			continue
		}
		offset, err := raw.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		files[directory] = workspaceIgnoreFile{offset: offset, size: header.Size, invalid: !regularIgnoreTarEntry(header)}
	}
	return files, nil
}

func regularIgnoreTarEntry(header *tar.Header) bool {
	if header.Typeflag != tar.TypeReg {
		return false
	}
	// Sparse payloads are reconstructed by tar.Reader and cannot be read by offset.
	for key := range header.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse.") {
			return false
		}
	}
	return true
}

func invalidWorkspaceIgnoreFile() error {
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_ignore",
		"workspace .gitignore files must be unique, regular, and within limits",
		"工作空间 .gitignore 文件必须为唯一的普通文件，且不能超出规则限制")
}

func skipWorkspaceBundleEntry(relative string, isDir bool, rules *sourceignore.Rules) bool {
	if rules == nil {
		return shouldSkipWorkspacePath(relative)
	}
	return skipDynamicWorkspaceNoise(relative) || path.Base(relative) == ".gitignore" || rules.Ignored(relative, isDir)
}

func skipDynamicWorkspaceNoise(relative string) bool {
	if shouldSkipWorkspacePath(relative) {
		return true
	}
	for segment := range strings.SplitSeq(relative, "/") {
		if segment == "deps" || segment == "venv" {
			return true
		}
	}
	return false
}
