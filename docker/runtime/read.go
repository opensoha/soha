package runtime

import (
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ReadProtocol = "docker-runtime-read-v1"

// ReadRequest never carries Compose, environment, bearer credentials or a host
// source path. The container binding comes from the control plane registry.
type ReadRequest struct {
	Protocol     string           `json:"protocol"`
	Action       string           `json:"action"`
	ProjectID    string           `json:"projectId"`
	Container    ContainerBinding `json:"container"`
	TailLines    int              `json:"tailLines,omitempty"`
	SinceSeconds int64            `json:"sinceSeconds,omitempty"`
	Target       string           `json:"target,omitempty"`
	Path         string           `json:"path,omitempty"`
	Limit        int              `json:"limit,omitempty"`
	LimitBytes   int64            `json:"limitBytes,omitempty"`
}

type Volume struct {
	Target            string `json:"target"`
	Type              string `json:"type"`
	ReadOnly          bool   `json:"readOnly"`
	BrowseSupported   bool   `json:"browseSupported"`
	UnsupportedReason string `json:"unsupportedReason,omitempty"`
}

type FileEntry struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	SizeBytes     int64  `json:"sizeBytes"`
	ModifiedAt    string `json:"modifiedAt,omitempty"`
	ReadSupported bool   `json:"readSupported"`
	ListSupported bool   `json:"listSupported"`
}

type ReadResult struct {
	Protocol  string           `json:"protocol"`
	Action    string           `json:"action"`
	ProjectID string           `json:"projectId"`
	Container ContainerBinding `json:"container"`
	Target    string           `json:"target,omitempty"`
	Path      string           `json:"path,omitempty"`
	Content   string           `json:"content,omitempty"`
	Encoding  string           `json:"encoding,omitempty"`
	SizeBytes int64            `json:"sizeBytes"`
	// ReturnedBytes is the UTF-8 payload length. For files.read, SizeBytes is
	// the source file's full size and can be larger when Truncated is true.
	ReturnedBytes int64       `json:"returnedBytes"`
	Truncated     bool        `json:"truncated"`
	Complete      bool        `json:"complete"`
	Volumes       []Volume    `json:"volumes"`
	Files         []FileEntry `json:"files"`
}

func (r ReadRequest) Normalized(action string) (ReadRequest, error) {
	if r.Protocol != ReadProtocol || r.Action != action || !SafeText(r.ProjectID, 128) || strings.TrimSpace(r.ProjectID) != r.ProjectID || r.ProjectID == "" || r.Container.Validate() != nil {
		return ReadRequest{}, errors.New("docker versioned read identity is invalid")
	}
	switch action {
	case "logs":
		return r.normalizedLogs()
	case "volumes":
		if r.Target != "" || r.Path != "" || r.Limit != 0 || r.LimitBytes != 0 || r.TailLines != 0 || r.SinceSeconds != 0 {
			return ReadRequest{}, errors.New("docker volume inventory does not accept file or log options")
		}
	case "files.list", "files.read":
		return r.normalizedFiles(action)
	default:
		return ReadRequest{}, errors.New("docker versioned read action is unsupported")
	}
	return r, nil
}

func (r ReadRequest) normalizedLogs() (ReadRequest, error) {
	if r.Target != "" || r.Path != "" || r.Limit != 0 || r.TailLines < 0 || r.TailLines > 2000 || r.SinceSeconds < 0 || r.SinceSeconds > 604800 || r.LimitBytes < 0 || r.LimitBytes > 1<<20 {
		return ReadRequest{}, errors.New("docker log read options are invalid")
	}
	if r.TailLines == 0 {
		r.TailLines = 200
	}
	if r.LimitBytes == 0 {
		r.LimitBytes = 1 << 20
	}
	return r, nil
}

func (r ReadRequest) normalizedFiles(action string) (ReadRequest, error) {
	if !ValidMountTarget(r.Target) || !ValidRelativePath(r.Path) || r.TailLines != 0 || r.SinceSeconds != 0 {
		return ReadRequest{}, errors.New("docker volume file target or path is invalid")
	}
	if action == "files.list" {
		if r.Limit < 0 || r.Limit > 1000 || r.LimitBytes != 0 {
			return ReadRequest{}, errors.New("docker directory read limit is invalid")
		}
		if r.Limit == 0 {
			r.Limit = 200
		}
	} else {
		if r.Limit != 0 || r.LimitBytes < 0 || r.LimitBytes > 1<<20 {
			return ReadRequest{}, errors.New("docker file read limit is invalid")
		}
		if r.LimitBytes == 0 {
			r.LimitBytes = 256 << 10
		}
	}
	if r.Path == "." {
		r.Path = ""
	}
	return r, nil
}

func SafeText(value string, maximum int) bool {
	if len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func ValidMountTarget(value string) bool {
	return SafeText(value, 4096) && strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value && !strings.Contains(value, `\`)
}

func ValidRelativePath(value string) bool {
	if !SafeText(value, 4096) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return false
	}
	if value == "" || value == "." {
		return true
	}
	return path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../")
}
