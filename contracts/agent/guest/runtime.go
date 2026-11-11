package guest

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	env "github.com/opensoha/soha-contracts/agent/environment"
)

const RuntimeOperationVersion = "opensoha.dev/personal-guest-runtime/v1"
const RuntimeResultVersion = "opensoha.dev/personal-guest-runtime-result/v1"
const ToolsPlanVersion = "opensoha.dev/personal-guest-tools/v1"
const RuntimeFileLimit = 44 << 10

var artifactDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runtimeImagePattern = regexp.MustCompile(`^docker\.io/library/debian@sha256:[a-f0-9]{64}$`)
var debianVersionPattern = regexp.MustCompile(`^[0-9][a-zA-Z0-9.+:~_-]{0,127}$`)

// Runtime operations are frozen by Core in an encrypted durable task. The SSH
// launcher carries only the operation identity; mTLS retrieves the exact grant.
type RuntimeOperation struct {
	SchemaVersion  string            `json:"schemaVersion"`
	OperationID    string            `json:"operationId"`
	Binding        Binding           `json:"binding"`
	Kind           string            `json:"kind"`
	Request        env.Request       `json:"request"`
	Deadline       time.Time         `json:"deadline"`
	File           *RuntimeFileInput `json:"file,omitempty"`
	Browser        *BrowserAction    `json:"browser,omitempty"`
	DockerImage    string            `json:"dockerImage,omitempty"`
	Source         string            `json:"source,omitempty"`
	RequestHash    string            `json:"requestHash,omitempty"`
	ArtifactSHA256 string            `json:"artifactSHA256"`
}

type RuntimeFileInput struct {
	Path           string `json:"path"`
	ContentBase64  string `json:"contentBase64,omitempty"`
	ExpectedSHA256 string `json:"expectedSHA256,omitempty"`
}

type RuntimeFileResult struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"contentBase64,omitempty"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
}

type BrowserAction struct {
	Action     string `json:"action"`
	ControlID  string `json:"controlId"`
	Generation int64  `json:"generation"`
	Owner      string `json:"owner"`
	TaskID     string `json:"taskId,omitempty"`
	URL        string `json:"url,omitempty"`
	Selector   string `json:"selector,omitempty"`
	Text       string `json:"text,omitempty"`
	X          *int   `json:"x,omitempty"`
	Y          *int   `json:"y,omitempty"`
}

type BrowserFrame struct {
	ControlID  string `json:"controlId"`
	Generation int64  `json:"generation"`
	InstanceID string `json:"instanceId"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text,omitempty"`
	JPEGBase64 string `json:"jpegBase64,omitempty"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

type RuntimeProof struct {
	InstanceID         string `json:"instanceId"`
	ArtifactSHA256     string `json:"artifactSHA256"`
	FilesystemID       string `json:"filesystemId"`
	Command            bool   `json:"command"`
	FileRead           bool   `json:"fileRead"`
	FileWrite          bool   `json:"fileWrite"`
	Browser            bool   `json:"browser"`
	Docker             bool   `json:"docker"`
	CredentialsHidden  bool   `json:"credentialsHidden"`
	NamespacesIsolated bool   `json:"namespacesIsolated"`
	DockerImage        string `json:"dockerImage"`
}

type RuntimeResult struct {
	SchemaVersion string             `json:"schemaVersion"`
	OperationID   string             `json:"operationId"`
	BindingID     string             `json:"bindingId"`
	Environment   env.Result         `json:"environment"`
	File          *RuntimeFileResult `json:"file,omitempty"`
	Browser       *BrowserFrame      `json:"browser,omitempty"`
	Proof         *RuntimeProof      `json:"proof,omitempty"`
}

// Tools installation cannot initialize data or accept commands/paths. Existing
// completed data and current owner authority are required before any host write.
type ToolsPlan struct {
	SchemaVersion     string  `json:"schemaVersion"`
	OperationID       string  `json:"operationId"`
	Binding           Binding `json:"binding"`
	ArtifactSHA256    string  `json:"artifactSHA256"`
	DiskQuotaBytes    int64   `json:"diskQuotaBytes"`
	CPUQuotaMilli     int64   `json:"cpuQuotaMilli"`
	MemoryBytes       int64   `json:"memoryBytes"`
	PidsLimit         int64   `json:"pidsLimit"`
	DockerImage       string  `json:"dockerImage"`
	BubblewrapVersion string  `json:"bubblewrapVersion"`
	DockerVersion     string  `json:"dockerVersion"`
	ChromiumVersion   string  `json:"chromiumVersion"`
}

type ToolsResult struct {
	OperationID    string `json:"operationId"`
	DataRef        string `json:"dataRef"`
	ArtifactSHA256 string `json:"artifactSHA256"`
}

func (p ToolsPlan) Validate(now time.Time) error {
	data := DataPlan{SchemaVersion: DataPlanVersion, OperationID: p.OperationID, Binding: p.Binding, DiskQuotaBytes: p.DiskQuotaBytes}
	quota := env.Quota{DiskBytes: p.DiskQuotaBytes, CPUUnits: p.CPUQuotaMilli, MemoryBytes: p.MemoryBytes, MaxProcesses: int(p.PidsLimit)}
	if p.SchemaVersion != ToolsPlanVersion || data.Validate(now) != nil || quota.ValidatePersonalDocker() != nil || !artifactDigestPattern.MatchString(p.ArtifactSHA256) || !runtimeImagePattern.MatchString(p.DockerImage) {
		return errors.New("invalid personal guest tools plan")
	}
	for _, v := range []string{p.BubblewrapVersion, p.DockerVersion, p.ChromiumVersion} {
		if !debianVersionPattern.MatchString(v) {
			return errors.New("invalid frozen Debian tools version")
		}
	}
	return nil
}

func RuntimeFilePath(path string) bool {
	if path == "" || len(path) > 1024 || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (op RuntimeOperation) Validate(now time.Time) error {
	if !artifactDigestPattern.MatchString(op.ArtifactSHA256) {
		return errors.New("invalid frozen runtime artifact")
	}
	if op.Source != "" && op.Source != "native" && op.Source != "user" || op.Source == "user" && !artifactDigestPattern.MatchString(op.RequestHash) || op.Source != "user" && op.RequestHash != "" {
		return errors.New("invalid frozen runtime origin")
	}
	if op.SchemaVersion != RuntimeOperationVersion || !uuidPattern.MatchString(op.OperationID) || op.Binding.Validate() != nil || now.Before(op.Binding.IssuedAt) || !now.Before(op.Binding.ExpiresAt) || op.Request.Validate() != nil || op.Request.Lease.OwnerID != op.Binding.AssistantID || op.Request.CreateData || !now.Before(op.Deadline) || op.Deadline.After(op.Request.Lease.ExpiresAt) || op.Deadline.After(op.Binding.ExpiresAt) || op.Deadline.Sub(now) > time.Minute {
		return errors.New("invalid or expired personal runtime operation")
	}
	if op.Request.Action == "delete_data" || op.Request.Action == "export_data" {
		return errors.New("runtime operation cannot dispose or export data")
	}
	return op.validateKind()
}

func (op RuntimeOperation) validateKind() error {
	switch op.Kind {
	case "environment":
		if op.File != nil || op.Browser != nil || op.DockerImage != "" || op.Request.Action == "execute" && op.Request.Lease.Mode != "restricted-write" {
			return errors.New("invalid environment runtime payload")
		}
	case "file_read", "file_write":
		return op.validateFile()
	case "docker":
		if op.File != nil || op.Browser != nil || !runtimeImagePattern.MatchString(op.DockerImage) || op.Request.Action != "execute" || op.Request.Lease.Mode != "restricted-write" {
			return errors.New("invalid Docker runtime payload")
		}
	case "browser":
		if op.Browser == nil || op.File != nil || op.DockerImage != "" || op.Request.Action != "inspect" {
			return errors.New("invalid browser runtime payload")
		}
		if (op.Browser.Action == "click" || op.Browser.Action == "type" || op.Browser.Action == "pointer") && op.Request.Lease.Mode != "restricted-write" {
			return errors.New("browser write requires a writable lease")
		}
		return op.Browser.Validate()
	default:
		return errors.New("unsupported personal runtime kind")
	}
	return nil
}

func (op RuntimeOperation) validateFile() error {
	if op.File == nil || op.Browser != nil || op.DockerImage != "" || op.Request.Action != "inspect" || !RuntimeFilePath(op.File.Path) {
		return errors.New("invalid personal runtime file")
	}
	if op.File.ExpectedSHA256 != "" && !artifactDigestPattern.MatchString(op.File.ExpectedSHA256) {
		return errors.New("invalid expected file identity")
	}
	if op.Kind == "file_read" {
		if op.File.ContentBase64 != "" || op.File.ExpectedSHA256 != "" {
			return errors.New("read cannot contain write material")
		}
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(op.File.ContentBase64)
	if err != nil || len(data) > RuntimeFileLimit || !utf8.Valid(data) || op.Request.Lease.Mode != "restricted-write" {
		return errors.New("invalid bounded personal file write")
	}
	return nil
}

func BrowserURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || len(value) > 4096 || u.User != nil || strings.ContainsAny(value, "\x00\r\n\\") {
		return false
	}
	if u.Scheme == "file" {
		return u.Host == "" && u.RawQuery == "" && strings.HasPrefix(u.Path, "/data/") && RuntimeFilePath(strings.TrimPrefix(u.Path, "/data/"))
	}
	return u.Scheme == "https" && u.Hostname() != "" && (u.Port() == "" || u.Port() == "443")
}

func (b BrowserAction) Validate() error {
	if !uuidPattern.MatchString(b.ControlID) || b.Generation < 1 || b.Generation > MaxCounter || (b.Owner != "user" && b.Owner != "agent") || (b.TaskID != "" && !uuidPattern.MatchString(b.TaskID)) || (b.Owner == "agent" && b.TaskID == "") || len(b.Selector) > 2048 || len(b.Text) > 4096 || !utf8.ValidString(b.Selector+b.Text) || strings.ContainsRune(b.Selector+b.Text, 0) {
		return errors.New("invalid browser control identity or input")
	}
	switch b.Action {
	case "navigate":
		if !BrowserURL(b.URL) || b.Selector != "" || b.Text != "" || b.X != nil || b.Y != nil {
			return errors.New("bounded browser target required")
		}
	case "click", "type":
		if b.URL != "" || b.Selector == "" || b.X != nil || b.Y != nil || b.Action == "click" && b.Text != "" {
			return errors.New("invalid selected browser action")
		}
	case "pointer":
		if b.Owner != "user" || b.URL != "" || b.Selector != "" || b.Text != "" || b.X == nil || b.Y == nil || *b.X < 0 || *b.X >= 800 || *b.Y < 0 || *b.Y >= 600 {
			return errors.New("invalid human browser pointer")
		}
	case "snapshot", "acquire", "takeover", "return", "release":
		if b.URL != "" || b.Selector != "" || b.Text != "" || b.X != nil || b.Y != nil {
			return errors.New("control action cannot carry a site operation")
		}
	default:
		return errors.New("unsupported browser action")
	}
	return nil
}

func (op RuntimeOperation) Digest() (string, error) {
	raw, err := json.Marshal(op)
	if err != nil || len(raw) > 320<<10 {
		return "", errors.New("personal runtime frame exceeds its bound")
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), nil
}

func (r RuntimeResult) ValidateFor(op RuntimeOperation) error {
	if r.SchemaVersion != RuntimeResultVersion || r.OperationID != op.OperationID || r.BindingID != op.Binding.ID || r.Environment.ValidateFor(op.Request) != nil {
		return errors.New("personal runtime receipt identity mismatch")
	}
	if r.Environment.ObservedAt.Before(op.Request.Lease.IssuedAt) || r.Environment.ObservedAt.After(op.Deadline.Add(5*time.Second)) {
		return errors.New("runtime receipt exceeds original authority window")
	}
	if op.Request.Action != "release" && r.Environment.State != "active" && r.Environment.State != "quota_exceeded" {
		return errors.New("runtime operation has no active observation")
	}
	if (op.Kind == "file_read" || op.Kind == "file_write") != (r.File != nil) || (op.Kind == "browser") != (r.Browser != nil) {
		return errors.New("personal runtime receipt kind mismatch")
	}
	if r.File != nil {
		f := r.File
		if f.Path != op.File.Path || !artifactDigestPattern.MatchString(f.SHA256) || f.Size < 0 || f.Size > RuntimeFileLimit {
			return errors.New("invalid personal file receipt")
		}
		if op.Kind == "file_read" {
			data, err := base64.StdEncoding.DecodeString(f.ContentBase64)
			if err != nil || int64(len(data)) != f.Size || !utf8.Valid(data) || fmt.Sprintf("%x", sha256.Sum256(data)) != f.SHA256 {
				return errors.New("personal file content identity mismatch")
			}
		} else if f.ContentBase64 != "" {
			return errors.New("write receipt must not echo content")
		} else {
			data, err := base64.StdEncoding.DecodeString(op.File.ContentBase64)
			if err != nil || f.Size != int64(len(data)) || f.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
				return errors.New("write receipt differs from frozen content")
			}
		}
	}
	if r.Browser != nil {
		b := r.Browser
		if b.ControlID != op.Browser.ControlID || b.Generation != op.Browser.Generation || !uuidPattern.MatchString(b.InstanceID) || len(b.JPEGBase64) > 48<<10 || len(b.Text) > 4096 || len(b.Title) > 512 || len(b.URL) > 4096 || b.Width != 800 || b.Height != 600 {
			return errors.New("invalid bounded browser receipt")
		}
		image, err := base64.StdEncoding.DecodeString(b.JPEGBase64)
		if err != nil {
			return errors.New("invalid browser pixels")
		}
		size, err := jpeg.DecodeConfig(bytes.NewReader(image))
		if err != nil || size.Width != 800 || size.Height != 600 || !utf8.ValidString(b.URL+b.Title+b.Text) || b.URL != "about:blank" && !BrowserURL(b.URL) {
			return errors.New("invalid browser frame or target")
		}
	}
	if r.Proof != nil && (op.Kind != "environment" || op.Request.Action != "inspect" || !uuidPattern.MatchString(r.Proof.InstanceID) || r.Proof.ArtifactSHA256 != op.ArtifactSHA256 || r.Proof.FilesystemID != op.Binding.DataRef || !runtimeImagePattern.MatchString(r.Proof.DockerImage)) {
		return errors.New("invalid personal runtime capability proof")
	}
	return nil
}
