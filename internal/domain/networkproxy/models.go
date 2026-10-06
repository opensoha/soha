package networkproxy

import "time"

const (
	EngineMihomo  = "mihomo"
	EngineSingBox = "sing-box"
	EngineV2Ray   = "v2ray"
)

type Instance struct {
	ID                      string     `json:"id"`
	Name                    string     `json:"name"`
	Engine                  string     `json:"engine"`
	Host                    string     `json:"host,omitempty"`
	EngineVersion           string     `json:"engineVersion,omitempty"`
	Status                  string     `json:"status"`
	Capabilities            []string   `json:"capabilities"`
	DesiredRevision         int64      `json:"desiredRevision"`
	ObservedRevision        int64      `json:"observedRevision"`
	LastSeenAt              *time.Time `json:"lastSeenAt,omitempty"`
	LastSampleAt            *time.Time `json:"lastSampleAt,omitempty"`
	ReasonCode              string     `json:"reasonCode,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
	Enabled                 bool       `json:"enabled"`
	Health                  string     `json:"-"`
	DesiredContentEncrypted string     `json:"-"`
	DesiredHash             string     `json:"-"`
	Registered              bool       `json:"-"`
}

type InstanceInput struct {
	ID     string
	Name   string
	Engine string
	Host   string
}

type ConfigurationInput struct {
	ExpectedRevision int64
	Enabled          bool
	Content          string
}

type Configuration struct {
	Revision    int64     `json:"revision"`
	Engine      string    `json:"engine"`
	Enabled     bool      `json:"enabled"`
	ContentHash string    `json:"contentHash"`
	Content     string    `json:"content"`
	ValidUntil  time.Time `json:"validUntil"`
}

type Applied struct {
	Revision     int64  `json:"revision"`
	Status       string `json:"status"`
	ReadbackHash string `json:"readbackHash"`
	ReasonCode   string `json:"reasonCode,omitempty"`
}

type Observation struct {
	Engine            string   `json:"engine"`
	EngineVersion     string   `json:"engineVersion"`
	Health            string   `json:"health"`
	Capabilities      []string `json:"capabilities"`
	ReasonCode        string   `json:"reasonCode,omitempty"`
	UptimeSeconds     int64    `json:"uptimeSeconds"`
	UploadTotal       int64    `json:"uploadTotal"`
	DownloadTotal     int64    `json:"downloadTotal"`
	ActiveConnections *int     `json:"activeConnections,omitempty"`
}

type TrafficSample struct {
	ObservedAt             time.Time `json:"observedAt"`
	UploadTotal            int64     `json:"uploadTotal"`
	DownloadTotal          int64     `json:"downloadTotal"`
	UploadBytesPerSecond   float64   `json:"uploadBytesPerSecond"`
	DownloadBytesPerSecond float64   `json:"downloadBytesPerSecond"`
	ActiveConnections      *int      `json:"activeConnections,omitempty"`
	UptimeSeconds          int64     `json:"-"`
}

type Traffic struct {
	InstanceID string          `json:"instanceId"`
	Supported  bool            `json:"supported"`
	Samples    []TrafficSample `json:"samples"`
}

type Connection struct {
	ID            string     `json:"id"`
	Destination   string     `json:"destination"`
	Network       string     `json:"network"`
	UploadBytes   int64      `json:"uploadBytes"`
	DownloadBytes int64      `json:"downloadBytes"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
}

type ConnectionsSnapshot struct {
	InstanceID  string       `json:"instanceId"`
	State       string       `json:"state"`
	ObservedAt  *time.Time   `json:"observedAt,omitempty"`
	Connections []Connection `json:"connections"`
}

type CloseCommand struct {
	ID           string    `json:"commandId"`
	InstanceID   string    `json:"-"`
	ConnectionID string    `json:"connectionId,omitempty"`
	Status       string    `json:"status"`
	ExpiresAt    time.Time `json:"expiresAt"`
	ReasonCode   string    `json:"reasonCode,omitempty"`
}
