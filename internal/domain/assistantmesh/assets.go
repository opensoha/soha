package assistantmesh

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
)

var ErrInvalidInput = errors.New("invalid assistant mesh input")

const (
	KindSkill          = "skill"
	KindExperienceCard = "experience_card"
	KindWorkPulse      = "work_pulse"

	maxTitleRunes    = 256
	maxBodyBytes     = 16 << 10
	maxBlockerRunes  = 1000
	maxAttachment    = 16 << 10
	maxAttachments   = 31
	maxPackageText   = 32 << 10
	maxDependencies  = 16
	maxResourcePath  = 128
	maxScopeMembers  = 100
	skillBodyName    = "skill.md"
	resourceNameExpr = `^[A-Za-z0-9][A-Za-z0-9._-]*$`
)

var (
	assetIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	assetVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	resourceSegment     = regexp.MustCompile(resourceNameExpr)
	parameterName       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	fileReference       = regexp.MustCompile(`(?i)\b(?:file|attachment|asset):`)
)

type AssetInput struct {
	Kind          string               `json:"kind"`
	Title         string               `json:"title"`
	Body          string               `json:"body"`
	Applicability string               `json:"applicability,omitempty"`
	Files         map[string]string    `json:"files,omitempty"`
	Dependencies  []Dependency         `json:"dependencies,omitempty"`
	Parameters    map[string]Parameter `json:"parameters,omitempty"`
	WorkStatus    string               `json:"workStatus,omitempty"`
	BlockerReason string               `json:"blockerReason,omitempty"`
}

type Dependency struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type Parameter struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func (in AssetInput) Validate() error {
	if err := in.validateKindFields(); err != nil {
		return err
	}
	if in.Kind != KindWorkPulse && (in.WorkStatus != "" || in.BlockerReason != "") {
		return invalid("this asset kind cannot define work pulse fields")
	}
	if strings.TrimSpace(in.Title) == "" {
		return invalid("title is required")
	}
	if utf8.RuneCountInString(in.Title) > maxTitleRunes {
		return invalid("title exceeds 256 characters")
	}
	if strings.TrimSpace(in.Body) == "" {
		return invalid("body is required")
	}
	if len(in.Body) > maxBodyBytes {
		return invalid("body exceeds 16 KiB")
	}
	if len(in.Applicability) > 4096 || !utf8.ValidString(in.Title+in.Body+in.Applicability+in.WorkStatus+in.BlockerReason) {
		return invalid("title, body, applicability and work pulse fields must be bounded UTF-8 text")
	}
	return nil
}

func (in AssetInput) validateKindFields() error {
	switch in.Kind {
	case KindSkill:
		if err := in.validateFiles(); err != nil {
			return err
		}
		if err := in.validateDependencies(); err != nil {
			return err
		}
		if err := in.validateParameters(); err != nil {
			return err
		}
	case KindExperienceCard:
		if in.Files != nil || in.Dependencies != nil || in.Parameters != nil {
			return invalid("experience cards cannot define skill resources")
		}
		if in.WorkStatus != "" || in.BlockerReason != "" {
			return invalid("experience cards cannot define work pulse fields")
		}
	case KindWorkPulse:
		if in.Files != nil || in.Dependencies != nil || in.Parameters != nil {
			return invalid("work pulses cannot define skill resources")
		}
		return in.validateWorkPulse()
	default:
		return invalid("kind must be skill, experience_card or work_pulse")
	}
	return nil
}

func (in AssetInput) validateWorkPulse() error {
	switch in.WorkStatus {
	case "in_progress", "blocked", "done":
	default:
		return invalid("work pulse status must be in_progress, blocked or done")
	}
	if in.BlockerReason != "" {
		if in.WorkStatus != "blocked" {
			return invalid("blocker reason requires blocked status")
		}
		if utf8.RuneCountInString(in.BlockerReason) > maxBlockerRunes || !utf8.ValidString(in.BlockerReason) ||
			strings.ContainsRune(in.BlockerReason, 0) || fileReference.MatchString(in.BlockerReason) {
			return invalid("blocker reason must be bounded plain UTF-8 text without file references")
		}
	}
	return nil
}

func (in AssetInput) validateFiles() error {
	if len(in.Files) > maxAttachments {
		return invalid("too many attachments")
	}
	seen := make(map[string]struct{}, len(in.Files))
	total := len(in.Body) + len(in.Applicability)
	for name, content := range in.Files {
		if !validResourceName(name) {
			return invalid("attachment path is invalid")
		}
		lower := strings.ToLower(name)
		if _, ok := seen[lower]; ok {
			return invalid("attachment paths must be unique regardless of case")
		}
		seen[lower] = struct{}{}
		if content == "" || len(content) > maxAttachment || !utf8.ValidString(content) {
			return invalid("attachment must contain UTF-8 text of up to 16 KiB")
		}
		if IsTextArchive(name) {
			if _, err := DecodeTextArchive(content); err != nil {
				return err
			}
		}
		total += len(content)
	}
	if total > maxPackageText {
		return invalid("skill package exceeds 32 KiB of text")
	}
	return nil
}

func validResourceName(name string) bool {
	if name == "" || len(name) > maxResourcePath || path.IsAbs(name) || path.Clean(name) != name {
		return false
	}
	segments := strings.Split(name, "/")
	for _, segment := range segments {
		if !resourceSegment.MatchString(segment) {
			return false
		}
	}
	base := segments[len(segments)-1]
	if strings.EqualFold(base, skillBodyName) {
		return false
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".csv", ".zip":
		return true
	default:
		return false
	}
}

func IsTextArchive(name string) bool {
	return strings.EqualFold(path.Ext(name), ".zip")
}

func DecodeTextArchive(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(encoded) > maxAttachment || !strings.HasPrefix(string(data), "PK\x03\x04") {
		return nil, invalid("ZIP attachment must use base64-encoded ZIP bytes")
	}
	return data, nil
}

func (in AssetInput) validateDependencies() error {
	if len(in.Dependencies) > maxDependencies {
		return invalid("too many dependencies")
	}
	seen := make(map[string]struct{}, len(in.Dependencies))
	for _, dependency := range in.Dependencies {
		if !assetIDPattern.MatchString(dependency.ID) || !assetVersionPattern.MatchString(dependency.Version) {
			return invalid("dependency id or version is invalid")
		}
		if _, ok := seen[dependency.ID]; ok {
			return invalid("dependency ids must be unique")
		}
		seen[dependency.ID] = struct{}{}
	}
	return nil
}

func (in AssetInput) validateParameters() error {
	if len(in.Parameters) > 32 {
		return invalid("too many parameters")
	}
	for name, parameter := range in.Parameters {
		if !parameterName.MatchString(name) || utf8.RuneCountInString(parameter.Description) > 256 || !utf8.ValidString(parameter.Description) {
			return invalid("parameter name or description is invalid")
		}
		switch parameter.Type {
		case "string", "integer", "number", "boolean":
		default:
			return invalid("parameter type is invalid")
		}
	}
	return nil
}

// Digest hashes the normalized asset payload, including every mutable field.
func (in AssetInput) Digest() string {
	payload, _ := json.Marshal(in)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func ValidateAudience(owner identity.Principal, scope knowledge.AccessScope) error {
	if strings.TrimSpace(owner.UserID) == "" {
		return invalid("publisher identity is required")
	}
	if len(scope.Users) > 0 || len(scope.Roles) > 0 {
		return invalid("audiences cannot grant users or roles")
	}
	if len(scope.Teams) > maxScopeMembers || len(scope.Projects) > maxScopeMembers {
		return invalid("audience grants are too large")
	}
	switch scope.Visibility {
	case "private", "organization":
		if len(scope.Teams)+len(scope.Projects) != 0 {
			return invalid("this visibility cannot select teams or projects")
		}
	case "teams":
		if len(scope.Teams) == 0 || len(scope.Projects) > 0 {
			return invalid("team audiences require only team grants")
		}
		if !validScopeMembers(scope.Teams) || !containsAll(owner.Teams, scope.Teams) {
			return invalid("team audiences must be teams the publisher belongs to")
		}
	case "projects":
		if len(scope.Projects) == 0 || len(scope.Teams) > 0 {
			return invalid("project audiences require only project grants")
		}
		if !validScopeMembers(scope.Projects) || !containsAll(owner.Projects, scope.Projects) {
			return invalid("project audiences must be projects the publisher belongs to")
		}
	default:
		return invalid("visibility must be private, teams, projects or organization")
	}
	return nil
}

func AudienceAllows(ownerID string, scope knowledge.AccessScope, caller identity.Principal) bool {
	if strings.TrimSpace(caller.UserID) == "" || len(scope.Users) > 0 || len(scope.Roles) > 0 {
		return false
	}
	switch scope.Visibility {
	case "private":
		return caller.UserID == ownerID && len(scope.Teams)+len(scope.Projects) == 0
	case "teams":
		return len(scope.Projects) == 0 && overlaps(caller.Teams, scope.Teams)
	case "projects":
		return len(scope.Teams) == 0 && overlaps(caller.Projects, scope.Projects)
	case "organization":
		return len(scope.Teams)+len(scope.Projects) == 0
	default:
		return false
	}
}

func validScopeMembers(members []string) bool {
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if member == "" || strings.TrimSpace(member) != member {
			return false
		}
		if _, ok := seen[member]; ok {
			return false
		}
		seen[member] = struct{}{}
	}
	return true
}

func containsAll(owned, selected []string) bool {
	allowed := make(map[string]struct{}, len(owned))
	for _, member := range owned {
		allowed[member] = struct{}{}
	}
	for _, member := range selected {
		if _, ok := allowed[member]; !ok {
			return false
		}
	}
	return true
}

func overlaps(left, right []string) bool {
	for _, item := range left {
		for _, candidate := range right {
			if item == candidate {
				return true
			}
		}
	}
	return false
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, message)
}
