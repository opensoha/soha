package networkaccess

import (
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	ingest "github.com/opensoha/soha/internal/domain/networkingest"
)

func mergeVPNMetrics(result *domain.VPNDashboard, gateways map[string]*domain.VPNGatewayMetrics, points map[time.Time]*domain.VPNSeriesPoint, metrics ingest.VPNMetrics) {
	result.TelemetryAvailable = true
	result.Partial = result.Partial || metrics.Partial
	result.AsOf = metrics.AsOf
	result.LatencyP50Ms, result.LatencyP95Ms = metrics.LatencyP50Ms, metrics.LatencyP95Ms
	bySession := map[string]ingest.VPNSessionMetrics{}
	for _, item := range metrics.Sessions {
		bySession[item.SessionID] = item
		addVPNCounter(&result.UploadBytes, item.UploadBytes)
		addVPNCounter(&result.DownloadBytes, item.DownloadBytes)
		// Historical sessions contribute bytes, but never today's displayed rate.
		for _, connection := range result.Sessions {
			if connection.SessionID == item.SessionID && connection.State == "connected" {
				addVPNRate(&result.UploadBytesPerSecond, item.UploadBytesPerSecond)
				addVPNRate(&result.DownloadBytesPerSecond, item.DownloadBytesPerSecond)
				if g := gateways[item.GatewayID]; g != nil {
					addVPNRate(&g.UploadBytesPerSecond, item.UploadBytesPerSecond)
					addVPNRate(&g.DownloadBytesPerSecond, item.DownloadBytesPerSecond)
				}
				break
			}
		}
	}
	for index := range result.Sessions {
		if item, ok := bySession[result.Sessions[index].SessionID]; ok {
			mergeVPNSessionMetrics(&result.Sessions[index], item)
		}
	}
	for _, item := range metrics.Gateways {
		if g := gateways[item.GatewayID]; g != nil {
			g.LatencyP50Ms, g.LatencyP95Ms = item.LatencyP50Ms, item.LatencyP95Ms
			g.MeasuredAt = &item.MeasuredAt
			if item.TrafficAvailable {
				g.UploadBytes, g.DownloadBytes = &item.UploadBytes, &item.DownloadBytes
			}
		}
	}
	for _, item := range metrics.Series {
		point := vpnSeriesPoint(points, item.At)
		point.LatencyP50Ms, point.LatencyP95Ms = item.LatencyP50Ms, item.LatencyP95Ms
		if item.TrafficAvailable {
			point.UploadBytes, point.DownloadBytes = &item.UploadBytes, &item.DownloadBytes
		}
	}
}

func mergeVPNSessionMetrics(connection *domain.VPNConnectionView, item ingest.VPNSessionMetrics) {
	connection.UploadBytes, connection.DownloadBytes = &item.UploadBytes, &item.DownloadBytes
	connection.MeasuredAt, connection.LastHandshakeAt = &item.MeasuredAt, item.LastHandshakeAt
	if connection.State == "connected" {
		connection.UploadBytesPerSecond, connection.DownloadBytesPerSecond = item.UploadBytesPerSecond, item.DownloadBytesPerSecond
	}
}
func addVPNCounter(target **int64, value int64) {
	if *target == nil {
		*target = new(int64)
	}
	**target += value
}
func addVPNRate(target **float64, value *float64) {
	if value == nil {
		return
	}
	if *target == nil {
		*target = new(float64)
	}
	**target += *value
}
