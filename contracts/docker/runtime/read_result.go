package runtime

import (
	"errors"
	"path"
	"strings"
	"unicode/utf8"
)

// ValidateFor binds an untrusted runtime response to the original normalized
// request. Callers must not accept a successful HTTP status as identity proof.
func (r ReadResult) ValidateFor(request ReadRequest) error {
	req, err := request.Normalized(request.Action)
	if err != nil || r.Protocol != ReadProtocol || r.Action != req.Action || r.ProjectID != req.ProjectID || r.Container != req.Container || r.Target != req.Target || r.Path != req.Path {
		return errors.New("docker read response identity differs from the original request")
	}
	if r.Volumes == nil || r.Files == nil || r.SizeBytes < 0 || r.ReturnedBytes < 0 || r.Complete == r.Truncated {
		return errors.New("docker read response completeness is invalid")
	}
	switch req.Action {
	case "logs", "files.read":
		return r.validateTextResult(req)
	case "volumes":
		return r.validateVolumes()
	case "files.list":
		return r.validateFiles(req)
	}
	return errors.New("docker read response action is unsupported")
}

func (r ReadResult) validateTextResult(req ReadRequest) error {
	if r.Encoding != "utf-8" || !utf8.ValidString(r.Content) || strings.ContainsRune(r.Content, 0) || int64(len(r.Content)) > req.LimitBytes || r.ReturnedBytes != int64(len(r.Content)) || len(r.Files) != 0 || len(r.Volumes) != 0 {
		return errors.New("docker read text response is invalid or exceeds its limit")
	}
	if req.Action == "logs" && r.SizeBytes != r.ReturnedBytes {
		return errors.New("docker log response byte count is invalid")
	}
	if req.Action == "files.read" && (r.SizeBytes < r.ReturnedBytes || r.Truncated != (r.SizeBytes > r.ReturnedBytes)) {
		return errors.New("docker file source size or truncation is invalid")
	}
	return nil
}

func (r ReadResult) validateVolumes() error {
	if len(r.Volumes) > 256 || len(r.Files) != 0 || r.Content != "" || r.Encoding != "" || r.SizeBytes != 0 || r.ReturnedBytes != 0 || !r.Complete {
		return errors.New("docker read volume response is invalid")
	}
	seen := map[string]bool{}
	for _, volume := range r.Volumes {
		if !ValidMountTarget(volume.Target) || seen[volume.Target] || !SafeText(volume.Type, 32) || volume.Type == "" || !SafeText(volume.UnsupportedReason, 256) || volume.BrowseSupported == (volume.UnsupportedReason != "") {
			return errors.New("docker read volume metadata is invalid")
		}
		seen[volume.Target] = true
	}
	return nil
}

func (r ReadResult) validateFiles(req ReadRequest) error {
	if len(r.Files) > req.Limit || len(r.Volumes) != 0 || r.Content != "" || r.Encoding != "" || r.SizeBytes != 0 || r.ReturnedBytes != 0 {
		return errors.New("docker read directory response is invalid")
	}
	seen := map[string]bool{}
	for _, file := range r.Files {
		if err := file.validate(req.Path); err != nil || seen[file.Name] {
			return errors.New("docker read file entry is invalid")
		}
		seen[file.Name] = true
	}
	return nil
}

func (f FileEntry) validate(parent string) error {
	if f.Name == "" || f.Name == "." || f.Name == ".." || strings.Contains(f.Name, "/") || !ValidRelativePath(f.Name) || f.Path != path.Join(parent, f.Name) || f.SizeBytes < 0 || !SafeText(f.ModifiedAt, 64) {
		return errors.New("docker file entry path or metadata is invalid")
	}
	switch f.Kind {
	case "file":
		if f.ListSupported {
			return errors.New("docker file cannot be listed as a directory")
		}
	case "directory":
		if f.ReadSupported {
			return errors.New("docker directory cannot be read as a file")
		}
	case "symlink", "other":
		if f.ReadSupported || f.ListSupported {
			return errors.New("docker special file access is unsupported")
		}
	default:
		return errors.New("docker file entry kind is unsupported")
	}
	return nil
}
