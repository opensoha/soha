package assistantmesh

import (
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/domain/knowledge"
)

// Asset is the owner-facing aggregate; shared callers only receive Published.
type Asset struct {
	ID                   string             `json:"id"`
	OwnerUserID          string             `json:"ownerUserId"`
	OriginalAuthorUserID string             `json:"originalAuthorUserId,omitempty"`
	Kind                 string             `json:"kind"`
	Draft                *domain.AssetInput `json:"draft,omitempty"`
	DraftDigest          string             `json:"draftDigest,omitempty"`
	Published            *Version           `json:"published,omitempty"`
	Review               *Review            `json:"review,omitempty"`
	Trial                *Trial             `json:"trial,omitempty"`
	Findings             []string           `json:"findings,omitempty"`
}

type Page struct {
	Items      []Asset            `json:"items"`
	NextOffset *int               `json:"nextOffset,omitempty"`
	Defaults   PublishingDefaults `json:"publishingDefaults"`
}

type PublishingDefaults struct {
	Visibility   string `json:"visibility"`
	LifetimeDays int    `json:"lifetimeDays"`
	Permanent    bool   `json:"allowPermanentExperienceCards"`
}

type Version struct {
	Input       domain.AssetInput     `json:"input"`
	Digest      string                `json:"digest"`
	Version     int                   `json:"version"`
	Audience    knowledge.AccessScope `json:"audience"`
	ExpiresAt   *time.Time            `json:"expiresAt,omitempty"`
	PublishedAt time.Time             `json:"publishedAt"`
}

type Review struct {
	Status    string  `json:"status"`
	Reason    string  `json:"reason,omitempty"`
	Candidate Version `json:"candidate"`
}

type Trial struct {
	RunID  string `json:"runId"`
	Digest string `json:"digest"`
	Status string `json:"status"`
}

type ProjectResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type ActionResult struct {
	Asset   Asset          `json:"asset"`
	RunID   string         `json:"runId,omitempty"`
	Project *ProjectResult `json:"project,omitempty"`
}

type ActionRequest struct {
	ExpectedDigest string                `json:"expectedDigest,omitempty"`
	Audience       knowledge.AccessScope `json:"audience,omitempty"`
	LifetimeHours  int                   `json:"lifetimeHours,omitempty"`
	LifetimeDays   int                   `json:"lifetimeDays,omitempty"`
	Permanent      bool                  `json:"permanent,omitempty"`
	Reason         string                `json:"reason,omitempty"`
	SessionID      string                `json:"sessionId,omitempty"`
	RequestID      string                `json:"requestId,omitempty"`
	Prompt         string                `json:"prompt,omitempty"`
	ResourceID     string                `json:"resourceId,omitempty"`
	Parameters     map[string]any        `json:"parameters,omitempty"`
}

// payload is the stored aggregate body. LastVersion keeps numbering monotonic
// across revoke so republished assets never reuse a version number.
type payload struct {
	OriginalAuthorID string             `json:"originalAuthorId,omitempty"`
	Draft            *domain.AssetInput `json:"draft,omitempty"`
	DraftDigest      string             `json:"draftDigest,omitempty"`
	Published        *Version           `json:"published,omitempty"`
	Review           *Review            `json:"review,omitempty"`
	Trial            *Trial             `json:"trial,omitempty"`
	Findings         []string           `json:"findings,omitempty"`
	LastVersion      int                `json:"lastVersion,omitempty"`
}
