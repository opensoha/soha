package assistantmesh

import (
	"errors"
	"time"
)

var ErrInquiryRateLimited = errors.New("inquiry rate limited")
var ErrInquiryInboxFull = errors.New("inquiry inbox full")

const DefaultInquiryPendingLimit = 20

type Profile struct {
	OwnerID string `json:"-"`
	Open    bool   `json:"open"`
	Away    bool   `json:"away"`
	Intro   string `json:"intro"`
}

type Citation struct {
	AssetID    string    `json:"assetId"`
	Version    int       `json:"version"`
	Title      string    `json:"title"`
	ObservedAt time.Time `json:"observedAt"`
}

type Inquiry struct {
	ID          string     `json:"id"`
	CallerID    string     `json:"callerId"`
	OwnerID     string     `json:"ownerId"`
	RequestID   string     `json:"-"`
	QuestionKey string     `json:"-"`
	Question    string     `json:"question"`
	Status      string     `json:"status"`
	Answer      string     `json:"answer,omitempty"`
	Citations   []Citation `json:"citations,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	DueAt       time.Time  `json:"dueAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
	PurgeAfter  *time.Time `json:"-"`
}

const (
	InquiryAnswered    = "answered"
	InquiryProcessing  = "processing"
	InquiryPending     = "pending"
	InquiryReplied     = "replied"
	InquiryExpired     = "expired"
	InquiryInvalidated = "invalidated"
)

type InquiryLimits struct {
	PairPerHour  int
	CallerPerDay int
	OwnerPending int
	MergeWindow  time.Duration
	Retention    time.Duration
}
