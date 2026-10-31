package contenttransfer

import (
	"time"

	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
)

type Policy struct {
	RequireStepUp    bool       `json:"requireStepUp,omitempty"`
	MinimumDataLevel string     `json:"minimumDataLevel,omitempty"`
	DetectorVersion  string     `json:"detectorVersion,omitempty"`
	NetworkAction    string     `json:"networkAction,omitempty"`
	Mode             string     `json:"mode"`
	Detectors        []string   `json:"detectors"`
	Rules            []dlp.Rule `json:"rules"`
}

func (p Policy) Scanner() dlp.Content {
	return dlp.Content{RequireStepUp: p.RequireStepUp, MinimumDataLevel: p.MinimumDataLevel, DetectorVersion: p.DetectorVersion, NetworkAction: p.NetworkAction, Enabled: true, Mode: p.Mode, Detectors: p.Detectors, Rules: p.Rules}
}

type Config struct {
	Name                 string   `json:"name"`
	Kind                 string   `json:"kind"`
	Enabled              bool     `json:"enabled"`
	Policy               Policy   `json:"policy"`
	StorageIntegrationID string   `json:"storageIntegrationId,omitempty"`
	BaseURL              string   `json:"baseUrl,omitempty"`
	SecretRef            string   `json:"secretRef,omitempty"`
	PrivateCIDRs         []string `json:"privateCIDRs,omitempty"`
}
type Destination struct {
	ID               string    `json:"id"`
	OwnerID          string    `json:"ownerId"`
	Revision         int64     `json:"revision"`
	Draft            Config    `json:"draft"`
	Published        *Config   `json:"published,omitempty"`
	PublishedVersion int64     `json:"publishedVersion"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type Summary struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	PublishedVersion int64  `json:"publishedVersion"`
	Mode             string `json:"mode"`
}
type SaveInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Draft            Config `json:"draft"`
}
type PublishInput struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}
type Authorization struct {
	AttemptID string `json:"attemptId"`
	Token     string `json:"token"`
}
type Request struct {
	SessionID     string         `json:"-"`
	RequestID     string         `json:"requestId"`
	Authorization *Authorization `json:"authorization,omitempty"`
}
type UploadInput struct {
	SessionID     string         `json:"-"`
	RequestID     string         `json:"requestId"`
	Authorization *Authorization `json:"authorization,omitempty"`
	DestinationID string         `json:"destinationId"`
	FileName      string         `json:"fileName"`
	Content       []byte         `json:"contentBase64"`
}
type PasteInput struct {
	SessionID     string         `json:"-"`
	RequestID     string         `json:"requestId"`
	Authorization *Authorization `json:"authorization,omitempty"`
	DestinationID string         `json:"destinationId"`
	FileName      string         `json:"fileName"`
	Text          string         `json:"text"`
}
type CopyInput struct {
	SessionID     string         `json:"-"`
	RequestID     string         `json:"requestId"`
	Authorization *Authorization `json:"authorization,omitempty"`
	DestinationID string         `json:"destinationId"`
}
type Gate struct {
	DestinationID string `json:"destinationId"`
	PolicyVersion int64  `json:"policyVersion"`
}

// Private never contains file bytes, names, excerpts or plaintext credentials.
type Private struct {
	NetworkRequested bool      `json:"networkRequested,omitempty"`
	RequestTag       []byte    `json:"requestTag"`
	Locator          string    `json:"locator"`
	Key              []byte    `json:"key"`
	Tag              []byte    `json:"tag"`
	Backends         []string  `json:"backends"`
	ObjectKey        string    `json:"objectKey"`
	Checksum         string    `json:"checksum"`
	ExpiresAt        time.Time `json:"expiresAt"`
	Deadline         time.Time `json:"deadline"`
	TokenHash        string    `json:"tokenHash,omitempty"`
	TokenExpiresAt   time.Time `json:"tokenExpiresAt"`
	TokenConsumed    bool      `json:"tokenConsumed"`
}
type Record struct {
	NetworkHoldID string        `json:"networkHoldId,omitempty"`
	NameRedacted  bool          `json:"nameRedacted"`
	ID            string        `json:"id"`
	ActorID       string        `json:"actorId"`
	DestinationID string        `json:"destinationId"`
	SourceID      string        `json:"sourceId,omitempty"`
	Action        string        `json:"action"`
	State         string        `json:"state"`
	Revision      int64         `json:"revision"`
	FileName      string        `json:"fileName"`
	SizeBytes     int64         `json:"sizeBytes"`
	Decision      *dlp.Decision `json:"decision,omitempty"`
	Gates         []Gate        `json:"gates"`
	AppealStatus  string        `json:"appealStatus"`
	AppealReason  string        `json:"appealReason,omitempty"`
	ReviewerID    string        `json:"reviewerId,omitempty"`
	ReviewReason  string        `json:"reviewReason,omitempty"`
	ErrorCode     string        `json:"errorCode,omitempty"`
	CreatedAt     time.Time     `json:"createdAt"`
	UpdatedAt     time.Time     `json:"updatedAt"`
	DeletedAt     *time.Time    `json:"deletedAt,omitempty"`
	Private       Private       `json:"-"`
}
type Result struct {
	Record  Record  `json:"record"`
	Content *[]byte `json:"contentBase64,omitempty"`
}
type Page struct {
	Items  []Record `json:"items"`
	Total  int64    `json:"total"`
	Offset int      `json:"offset"`
	Limit  int      `json:"limit"`
}
type ReviewInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Resolution       string `json:"resolution"`
	ReasonCode       string `json:"reasonCode"`
}

type NetworkHold struct {
	ID             string              `json:"id"`
	SourceRecordID string              `json:"sourceRecordId"`
	DestinationID  string              `json:"destinationId"`
	SubjectID      string              `json:"subjectId"`
	Active         bool                `json:"active"`
	Revision       int64               `json:"revision"`
	CreatedAt      time.Time           `json:"createdAt"`
	ReleasedAt     *time.Time          `json:"releasedAt,omitempty"`
	Targets        []dlp.NetworkTarget `json:"targets"`
}
