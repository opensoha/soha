package networkaccess

import "time"

type VPNScope struct {
	ProfileID      string `json:"profileId"`
	SiteID         string `json:"siteId"`
	NetworkSpaceID string `json:"networkSpaceId"`
}

type VPNDashboardFilter struct {
	From, To                                                      time.Time
	ProfileID, SiteID, GatewayID, SubjectID, TeamID, ProviderCode string
	Limit                                                         int
}

type VPNDashboardQuery struct {
	VPNDashboardFilter
	Scopes     []VPNScope
	DeviceID   string
	DecisionID string
}

type VPNConnectionView struct {
	SessionID              string     `json:"sessionId"`
	ProfileID              string     `json:"profileId"`
	ProfileName            string     `json:"profileName"`
	SubjectID              string     `json:"subjectId"`
	DeviceID               string     `json:"deviceId"`
	GatewayID              string     `json:"gatewayId"`
	GatewayName            string     `json:"gatewayName"`
	ProviderCode           string     `json:"providerCode"`
	Selection              string     `json:"selection"`
	Mode                   string     `json:"mode"`
	State                  string     `json:"state"`
	ReasonCode             string     `json:"reasonCode"`
	StartedAt              time.Time  `json:"startedAt"`
	EndedAt                *time.Time `json:"endedAt,omitempty"`
	DecisionID             string     `json:"decisionId"`
	TunnelIP               string     `json:"tunnelIP,omitempty"`
	LastHandshakeAt        *time.Time `json:"lastHandshakeAt,omitempty"`
	UploadBytes            *int64     `json:"uploadBytes,omitempty"`
	DownloadBytes          *int64     `json:"downloadBytes,omitempty"`
	UploadBytesPerSecond   *float64   `json:"uploadBytesPerSecond,omitempty"`
	DownloadBytesPerSecond *float64   `json:"downloadBytesPerSecond,omitempty"`
	LatencyMs              *float64   `json:"latencyMs,omitempty"`
	MeasuredAt             *time.Time `json:"measuredAt,omitempty"`
	RuntimeID              string     `json:"-"`
	GatewayRuntimeID       string     `json:"-"`
}

type VPNDashboardRecord struct {
	Decision      VPNDecision
	Connection    *VPNConnectionView
	RuntimeID     string
	EstablishedAt *time.Time
}

type VPNGatewayMetrics struct {
	GatewayID              string     `json:"gatewayId"`
	Name                   string     `json:"name"`
	Region                 string     `json:"region"`
	ProviderCode           string     `json:"providerCode"`
	ProviderName           string     `json:"providerName"`
	Healthy                *bool      `json:"healthy,omitempty"`
	AcceptNewConnections   *bool      `json:"acceptNewConnections,omitempty"`
	MaxSessions            *int       `json:"maxSessions,omitempty"`
	ActiveSessions         int        `json:"activeSessions"`
	Attempts               int        `json:"attempts"`
	Successes              int        `json:"successes"`
	Fallbacks              int        `json:"fallbacks"`
	UploadBytes            *int64     `json:"uploadBytes,omitempty"`
	DownloadBytes          *int64     `json:"downloadBytes,omitempty"`
	UploadBytesPerSecond   *float64   `json:"uploadBytesPerSecond,omitempty"`
	DownloadBytesPerSecond *float64   `json:"downloadBytesPerSecond,omitempty"`
	LatencyP50Ms           *float64   `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms           *float64   `json:"latencyP95Ms,omitempty"`
	MeasuredAt             *time.Time `json:"measuredAt,omitempty"`
}

type VPNSeriesPoint struct {
	At            time.Time `json:"at"`
	Attempts      int       `json:"attempts"`
	Successes     int       `json:"successes"`
	Fallbacks     int       `json:"fallbacks"`
	UploadBytes   *int64    `json:"uploadBytes,omitempty"`
	DownloadBytes *int64    `json:"downloadBytes,omitempty"`
	LatencyP50Ms  *float64  `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms  *float64  `json:"latencyP95Ms,omitempty"`
}

type VPNDashboard struct {
	AsOf                   time.Time           `json:"asOf"`
	From                   time.Time           `json:"from"`
	To                     time.Time           `json:"to"`
	TelemetryAvailable     bool                `json:"telemetryAvailable"`
	Partial                bool                `json:"partial"`
	ActiveSessions         int                 `json:"activeSessions"`
	AvailableGateways      int                 `json:"availableGateways"`
	TotalGateways          int                 `json:"totalGateways"`
	Attempts               int                 `json:"attempts"`
	Successes              int                 `json:"successes"`
	Fallbacks              int                 `json:"fallbacks"`
	UploadBytes            *int64              `json:"uploadBytes,omitempty"`
	DownloadBytes          *int64              `json:"downloadBytes,omitempty"`
	UploadBytesPerSecond   *float64            `json:"uploadBytesPerSecond,omitempty"`
	DownloadBytesPerSecond *float64            `json:"downloadBytesPerSecond,omitempty"`
	LatencyP50Ms           *float64            `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms           *float64            `json:"latencyP95Ms,omitempty"`
	Gateways               []VPNGatewayMetrics `json:"gateways"`
	Series                 []VPNSeriesPoint    `json:"series"`
	Sessions               []VPNConnectionView `json:"sessions"`
	Decisions              []VPNDecision       `json:"decisions"`
}

type VPNPreviewInput struct {
	DeviceID     string `json:"deviceId"`
	ProfileID    string `json:"profileId"`
	Selection    string `json:"selection"`
	GatewayID    string `json:"gatewayId,omitempty"`
	ProbeBatchID string `json:"probeBatchId,omitempty"`
}
