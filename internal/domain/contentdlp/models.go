package contentdlp

import "time"

const MaxTextBytes = 256 * 1024
const DetectorVersion = "content-dlp-text-v1"
const ArchiveDetectorVersion = "content-dlp-archive-v2"

type Rule struct {
	ID         string `json:"id"`
	Expression string `json:"expression"`
	DataLevel  string `json:"dataLevel,omitempty"`
}

type Content struct {
	RequireStepUp    bool     `json:"requireStepUp,omitempty"`
	MinimumDataLevel string   `json:"minimumDataLevel,omitempty"`
	DetectorVersion  string   `json:"detectorVersion,omitempty"`
	NetworkAction    string   `json:"networkAction,omitempty"`
	Enabled          bool     `json:"enabled"`
	Mode             string   `json:"mode"`
	Detectors        []string `json:"detectors"`
	Rules            []Rule   `json:"rules"`
}

type Policy struct {
	ProjectID        string    `json:"projectId"`
	Revision         int64     `json:"revision"`
	Draft            Content   `json:"draft"`
	Published        *Content  `json:"published,omitempty"`
	PublishedVersion int64     `json:"publishedVersion"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type SaveInput struct {
	ExpectedRevision int64   `json:"expectedRevision"`
	Draft            Content `json:"draft"`
}

type PublishInput struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

type Decision struct {
	StepUpRequired     bool     `json:"stepUpRequired,omitempty"`
	DataLevel          string   `json:"dataLevel,omitempty"`
	MinimumDataLevel   string   `json:"minimumDataLevel,omitempty"`
	RuleSetVersion     string   `json:"ruleSetVersion,omitempty"`
	RequiredAction     string   `json:"requiredAction,omitempty"`
	AuthorizationKind  string   `json:"authorizationKind,omitempty"`
	OriginEventID      string   `json:"originEventId,omitempty"`
	EventID            string   `json:"eventId"`
	Mode               string   `json:"mode"`
	Status             string   `json:"status"`
	Allowed            bool     `json:"allowed"`
	PolicyVersion      int64    `json:"policyVersion"`
	DetectorVersion    string   `json:"detectorVersion"`
	ContentFingerprint string   `json:"contentFingerprint,omitempty"`
	MatchedRuleIDs     []string `json:"matchedRuleIds"`
}

// Event deliberately contains neither content nor matched excerpts or file paths.
type Event struct {
	NetworkHoldID    string    `json:"networkHoldId,omitempty"`
	ProjectUpdatedAt time.Time `json:"-"`
	Decision
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	ActorID           string    `json:"actorId"`
	Action            string    `json:"action"`
	Revision          int64     `json:"revision"`
	TargetFingerprint string    `json:"targetFingerprint,omitempty"`
	SizeBytes         int64     `json:"sizeBytes"`
	InspectedBytes    int64     `json:"inspectedBytes"`
	CreatedAt         time.Time `json:"createdAt"`
}

func DefaultPolicy(projectID string) Policy {
	return Policy{ProjectID: projectID, Draft: Content{Mode: "audit", Detectors: []string{"private_key", "aws_access_key", "credential_assignment"}, Rules: []Rule{}}}
}
