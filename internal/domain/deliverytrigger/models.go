package deliverytrigger

import (
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"time"
)

type Trigger = sohaapi.DeliveryTrigger
type Input = sohaapi.DeliveryTriggerInput
type Event = sohaapi.DeliveryTriggerEvent
type Schedule = sohaapi.DeliveryTriggerSchedule
type Webhook = sohaapi.DeliveryTriggerWebhook

type StoredTrigger struct {
	Trigger
	ExecutionTokenID string `json:"-"`
	UpdatedByTokenID string `json:"-"`
	TargetDigest     string `json:"-"`
	SigningSecret    string `json:"-"`
	LastBatchID      string `json:"-"`
}

type StoredEvent struct {
	Event
	PayloadDigest    string
	SourceGeneration int
	Lease            string
	ClaimedAt        time.Time
	PreparedAt       *time.Time
}
