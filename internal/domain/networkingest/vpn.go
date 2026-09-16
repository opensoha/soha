package networkingest

import "time"

// Bindings are produced by the management service after object-scope checks.
// Ingest only joins events whose authenticated producer and session both match.
type VPNSessionBinding struct {
	SessionID         string `json:"sessionId"`
	ProfileID         string `json:"profileId"`
	GatewayID         string `json:"gatewayId"`
	GatewayRuntimeID  string `json:"gatewayRuntimeId"`
	EndpointRuntimeID string `json:"endpointRuntimeId"`
}

type VPNProbeBinding struct {
	IntentID          string   `json:"intentId"`
	ProfileID         string   `json:"profileId"`
	EndpointRuntimeID string   `json:"endpointRuntimeId"`
	GatewayIDs        []string `json:"gatewayIds"`
}

type VPNMetricsQuery struct {
	From     time.Time           `json:"from"`
	To       time.Time           `json:"to"`
	Sessions []VPNSessionBinding `json:"sessions"`
	Probes   []VPNProbeBinding   `json:"probes"`
}

type VPNSessionMetrics struct {
	SessionID              string     `json:"sessionId"`
	GatewayID              string     `json:"gatewayId"`
	UploadBytes            int64      `json:"uploadBytes"`
	DownloadBytes          int64      `json:"downloadBytes"`
	UploadBytesPerSecond   *float64   `json:"uploadBytesPerSecond,omitempty"`
	DownloadBytesPerSecond *float64   `json:"downloadBytesPerSecond,omitempty"`
	LastHandshakeAt        *time.Time `json:"lastHandshakeAt,omitempty"`
	MeasuredAt             time.Time  `json:"measuredAt"`
}

type VPNGatewayMetrics struct {
	TrafficAvailable bool      `json:"trafficAvailable"`
	GatewayID        string    `json:"gatewayId"`
	UploadBytes      int64     `json:"uploadBytes"`
	DownloadBytes    int64     `json:"downloadBytes"`
	LatencyP50Ms     *float64  `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms     *float64  `json:"latencyP95Ms,omitempty"`
	SampleCount      int       `json:"sampleCount"`
	MeasuredAt       time.Time `json:"measuredAt"`
}

type VPNMetricPoint struct {
	TrafficAvailable bool      `json:"trafficAvailable"`
	At               time.Time `json:"at"`
	UploadBytes      int64     `json:"uploadBytes"`
	DownloadBytes    int64     `json:"downloadBytes"`
	LatencyP50Ms     *float64  `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms     *float64  `json:"latencyP95Ms,omitempty"`
}

type VPNMetrics struct {
	From         time.Time           `json:"from"`
	To           time.Time           `json:"to"`
	AsOf         time.Time           `json:"asOf"`
	Partial      bool                `json:"partial"`
	Sessions     []VPNSessionMetrics `json:"sessions"`
	Gateways     []VPNGatewayMetrics `json:"gateways"`
	Series       []VPNMetricPoint    `json:"series"`
	LatencyP50Ms *float64            `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms *float64            `json:"latencyP95Ms,omitempty"`
	SampleCount  int                 `json:"sampleCount"`
}
