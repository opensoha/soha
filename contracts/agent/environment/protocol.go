// Package environment carries the Core-to-Agent personal environment protocol.
// JSON Schemas in agent/ own the wire shapes; Docker handles stay Agent-private.
package environment

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const RequestVersion = "opensoha.dev/personal-environment-request/v1"
const ResultVersion = "opensoha.dev/personal-environment-result/v1"
const LeaseVersion = "opensoha.dev/environment-lease/v1"
const SnapshotVersion = "opensoha.dev/environment-snapshot/v1"

type Quota struct {
	DiskBytes     int64 `json:"diskBytes"`
	MemoryBytes   int64 `json:"memoryBytes"`
	CPUUnits      int64 `json:"cpuUnits"`
	MaxProcesses  int   `json:"maxProcesses"`
	NetworkEgress bool  `json:"networkEgress"`
}

type Lease struct {
	SchemaVersion string     `json:"schemaVersion"`
	ID            string     `json:"id"`
	RunID         string     `json:"runId"`
	OwnerID       string     `json:"ownerId"`
	ScopeHash     string     `json:"scopeHash"`
	Mode          string     `json:"mode"`
	WorkspacePath string     `json:"workspacePath,omitempty"`
	Quota         Quota      `json:"quota"`
	IssuedAt      time.Time  `json:"issuedAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	ReleasedAt    *time.Time `json:"releasedAt,omitempty"`
}

type Usage struct {
	DiskBytes    int64 `json:"diskBytes"`
	MemoryBytes  int64 `json:"memoryBytes"`
	CPUTimeNanos int64 `json:"cpuTimeNanos"`
	Processes    int   `json:"processes"`
}

type Snapshot struct {
	SchemaVersion string    `json:"schemaVersion"`
	LeaseID       string    `json:"leaseId"`
	RunID         string    `json:"runId"`
	State         string    `json:"state"`
	ResourceUsage Usage     `json:"resourceUsage"`
	ArtifactRefs  []string  `json:"artifactRefs,omitempty"`
	ObservedAt    time.Time `json:"observedAt"`
}

// Request is sent only by the trusted Core connection. Owner, scope, quota and
// the original lease timestamps are server-derived, never user-selected.
// Execute is not retryable: a lost receipt must be treated as an unknown outcome.
type Request struct {
	SchemaVersion string   `json:"schemaVersion"`
	Action        string   `json:"action"`
	Lease         Lease    `json:"lease"`
	Argv          []string `json:"argv,omitempty"`
	// TaskID selects a server-owned temporary directory inside the sandbox.
	// It never changes the personal owner, lease or persistent mount.
	TaskID string `json:"taskId,omitempty"`
	// Core permits this only before the first successful provisioning receipt.
	CreateData bool `json:"createData,omitempty"`
}

type Result struct {
	SchemaVersion string    `json:"schemaVersion"`
	LeaseID       string    `json:"leaseId"`
	RunID         string    `json:"runId"`
	TaskID        string    `json:"taskId,omitempty"`
	State         string    `json:"state"`
	Snapshot      *Snapshot `json:"snapshot,omitempty"`
	Output        *string   `json:"output,omitempty"`
	ExitCode      *int      `json:"exitCode,omitempty"`
	ObservedAt    time.Time `json:"observedAt"`
}

var scopePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var canonicalUUIDPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func (r *Request) UnmarshalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("environment request must be UTF-8")
	}
	type wireRequest Request
	var decoded wireRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		return err
	}
	if _, present := properties["argv"]; present && decoded.Action != "execute" {
		return errors.New("only execute accepts command arguments")
	}
	if _, present := properties["taskId"]; present && (decoded.Action != "execute" || !canonicalUUIDPattern.MatchString(decoded.TaskID)) {
		return errors.New("only execute accepts a canonical server task identity")
	}
	*r = Request(decoded)
	return r.Validate()
}

func (r Request) Validate() error {
	l := r.Lease
	if r.SchemaVersion != RequestVersion || l.SchemaVersion != LeaseVersion || !scopePattern.MatchString(l.ScopeHash) {
		return errors.New("invalid environment protocol identity")
	}
	for _, id := range []string{l.ID, l.RunID, l.OwnerID} {
		if strings.TrimSpace(id) == "" || utf8.RuneCountInString(id) > 128 || !utf8.ValidString(id) || strings.ContainsRune(id, '\x00') {
			return errors.New("invalid environment lease identity")
		}
	}
	if (l.Mode != "observe-only" && l.Mode != "restricted-write") || l.WorkspacePath != "" || l.ReleasedAt != nil || l.IssuedAt.IsZero() || !l.ExpiresAt.After(l.IssuedAt) || l.ExpiresAt.Sub(l.IssuedAt) > 24*time.Hour {
		return errors.New("invalid original personal environment lease")
	}
	if err := l.Quota.ValidatePersonalDocker(); err != nil {
		return err
	}
	return r.validateAction()
}

func (q Quota) ValidatePersonalDocker() error {
	if q.DiskBytes < 16<<20 || q.DiskBytes > 1<<40 || q.MemoryBytes < 32<<20 || q.MemoryBytes > 16<<30 || q.CPUUnits < 1 || q.CPUUnits > 8000 || q.MaxProcesses < 4 || q.MaxProcesses > 256 || q.NetworkEgress {
		return errors.New("unsupported personal environment quota")
	}
	return nil
}

func (r Request) validateAction() error {
	if r.TaskID != "" && (r.Action != "execute" || !canonicalUUIDPattern.MatchString(r.TaskID)) {
		return errors.New("invalid personal execution task identity")
	}
	if r.CreateData && r.Action != "acquire" {
		return errors.New("only initial acquisition can create persistent data")
	}
	if r.Action != "execute" {
		if (r.Action != "acquire" && r.Action != "inspect" && r.Action != "release" && r.Action != "delete_data" && r.Action != "export_data") || r.Argv != nil {
			return errors.New("invalid environment action")
		}
		return nil
	}
	if len(r.Argv) == 0 || len(r.Argv) > 64 || !strings.HasPrefix(r.Argv[0], "/") {
		return errors.New("bounded absolute sandbox command required")
	}
	for _, arg := range r.Argv {
		if len(arg) > 4096 || !utf8.ValidString(arg) || strings.ContainsRune(arg, '\x00') {
			return errors.New("invalid sandbox command argument")
		}
	}
	return nil
}

// ValidateFor binds a receipt to the dispatched operation. Stopped or missing
// containers cannot claim fresh resource measurements or successful commands.
func (result Result) ValidateFor(request Request) error {
	if result.ExitCode != nil && (request.Action != "execute" || *result.ExitCode < 0 || *result.ExitCode > 255) {
		return errors.New("invalid command exit receipt")
	}
	if request.Action == "export_data" {
		return errors.New("export requires its complete binary stream receipt")
	}
	if result.SchemaVersion != ResultVersion || result.LeaseID != request.Lease.ID || result.RunID != request.Lease.RunID || result.TaskID != request.TaskID || result.ObservedAt.IsZero() {
		return errors.New("environment receipt identity mismatch")
	}
	active := result.State == "active" || result.State == "quota_exceeded"
	if !active && result.State != "released" && result.State != "expired" && result.State != "missing" && result.State != "deleted" {
		return errors.New("invalid environment receipt state")
	}
	if request.Action == "release" && (result.State != "released" && result.State != "missing") {
		return errors.New("release has no stopped receipt")
	}
	if request.Action == "delete_data" && result.State != "deleted" {
		return errors.New("data deletion has no confirmed receipt")
	}
	if (request.Action == "acquire" || request.Action == "execute") && !active {
		return errors.New("operation has no active receipt")
	}
	if !active {
		if result.Snapshot != nil || result.Output != nil {
			return errors.New("inactive receipt contains invented observation")
		}
		return nil
	}
	if err := result.validateObservation(); err != nil {
		return err
	}
	if (request.Action == "execute") != (result.Output != nil) || (result.Output != nil && len(*result.Output) > 64<<10) {
		return errors.New("invalid command receipt")
	}
	return nil
}

func (result Result) validateObservation() error {
	s := result.Snapshot
	if s == nil || s.SchemaVersion != SnapshotVersion || s.LeaseID != result.LeaseID || s.RunID != result.RunID || s.State != result.State || !s.ObservedAt.Equal(result.ObservedAt) {
		return errors.New("environment observation identity mismatch")
	}
	u := s.ResourceUsage
	if u.DiskBytes < 0 || u.MemoryBytes < 0 || u.CPUTimeNanos < 0 || u.Processes < 0 || len(s.ArtifactRefs) > 128 {
		return errors.New("invalid environment observation")
	}
	seen := make(map[string]bool, len(s.ArtifactRefs))
	for _, ref := range s.ArtifactRefs {
		if utf8.RuneCountInString(ref) > 512 || seen[ref] {
			return errors.New("invalid environment artifact reference")
		}
		seen[ref] = true
	}
	return nil
}
