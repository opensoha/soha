package networkproxy

import "time"
import api "github.com/opensoha/soha-contracts/gen/go/sohaapi"

type RuntimeCommandInput = api.NetworkProxyRuntimeCommandInput
type RuntimeCommandReceipt = api.NetworkProxyRuntimeCommandReceipt
type CommandDataResult = api.NetworkProxyRuntimeCommandResult
type CloseInput = api.NetworkProxyCloseInput

type RuntimeCommand struct {
	CommandID string              `json:"commandId"`
	Input     RuntimeCommandInput `json:"input"`
}
type RuntimeCommandResult struct {
	Kind         string             `json:"kind"`
	ExpiresAt    time.Time          `json:"expiresAt"`
	CommandID    string             `json:"commandId"`
	RequestID    string             `json:"requestId"`
	ProcessEpoch string             `json:"processEpoch"`
	Status       string             `json:"status"`
	ReasonCode   string             `json:"reasonCode,omitempty"`
	Result       *CommandDataResult `json:"result,omitempty"`
}

type DesiredBatch struct {
	PublicationID string
	MasterID      string
	MemberSetHash string
	OwnerEpoch    int64
	Members       []DesiredMember
}
type DesiredMember struct {
	InstanceID       string
	ExpectedRevision int64
	Enabled          bool
	Content          *string
	Binding          *Binding
	BundleID         string
	BundleRevision   int64
	Encrypted        string
	ContentHash      string
}
type MemberConfigurationState struct {
	ConfigurationValidUntil *time.Time
	LastSeenAt              *time.Time
	LastSampleAt            *time.Time
	BundleState             string
	InstanceID              string
	DesiredRevision         int64
	AppliedRevision         int64
	ContentHash             string
	DesiredHash             string
	AppliedHash             string
	PolicyVersion           int
	ObservedPolicyVersion   int
	ProcessEpoch            string
	BundleID                string
	BundleRevision          int64
}

type DesiredBatchReceipt struct {
	PublicationID string
	MasterID      string
	IntentHash    string
	MemberSetHash string
	OwnerEpoch    int64
	CommittedAt   time.Time
	Members       []MemberConfigurationState
}
