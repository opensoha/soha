package networkingest

import (
	"encoding/json"
	"time"
)

type Event struct {
	ProducerID   string
	EventID      string
	EventHash    string
	BatchID      string
	ProducerKind string
	EventType    string
	Sequence     int64
	OccurredAt   time.Time
	ReceivedAt   time.Time
	Payload      json.RawMessage
}

type Result struct {
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
}

type Acknowledgement struct {
	BatchID   string `json:"batchId"`
	Accepted  int    `json:"accepted"`
	Duplicate int    `json:"duplicate"`
}

type SummaryFilter struct {
	From       time.Time
	To         time.Time
	ProducerID string
	Limit      int
}

type ProducerSummary struct {
	ProducerID      string    `json:"producerId"`
	ProducerKind    string    `json:"producerKind"`
	LastSeenAt      time.Time `json:"lastSeenAt"`
	GapCount        int64     `json:"gapCount"`
	RegressionCount int64     `json:"regressionCount"`
}

type ProxyFlowSummary struct {
	ProducerID        string    `json:"producerId"`
	Engine            string    `json:"engine"`
	ProfileID         string    `json:"profileId"`
	ProfileRevision   int       `json:"profileRevision"`
	Mode              string    `json:"mode"`
	SelectedProxy     string    `json:"selectedProxy"`
	UploadBytes       int64     `json:"uploadBytes"`
	DownloadBytes     int64     `json:"downloadBytes"`
	ActiveConnections int64     `json:"activeConnections"`
	LastOccurredAt    time.Time `json:"lastOccurredAt"`
}

type Summary struct {
	From                   time.Time          `json:"from"`
	To                     time.Time          `json:"to"`
	EventCount             int64              `json:"eventCount"`
	HeartbeatCount         int64              `json:"heartbeatCount"`
	RadiusAccountingCount  int64              `json:"radiusAccountingCount"`
	NetworkFlowCount       int64              `json:"networkFlowCount"`
	ConnectionSummaryCount int64              `json:"connectionSummaryCount"`
	ProxyFlowCount         int64              `json:"proxyFlowCount"`
	UploadBytes            int64              `json:"uploadBytes"`
	DownloadBytes          int64              `json:"downloadBytes"`
	ActiveConnections      int64              `json:"activeConnections"`
	Producers              []ProducerSummary  `json:"producers"`
	ProxyFlows             []ProxyFlowSummary `json:"proxyFlows"`
}
