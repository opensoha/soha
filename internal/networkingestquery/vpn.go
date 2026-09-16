package networkingestquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func (client *Client) VPNProbe(ctx context.Context, producerID, intentID, batchID string) (*networkprotocol.VPNProbeBatch, error) {
	query := url.Values{"producerId": {producerID}, "intentId": {intentID}, "batchId": {batchID}}
	var envelope struct {
		Data *networkprotocol.VPNProbeBatch `json:"data"`
	}
	if err := client.vpnRequest(ctx, http.MethodGet, "/api/ingest/v1/query/vpn/probe?"+query.Encode(), nil, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data != nil && (envelope.Data.BatchID != batchID || (intentID != "" && envelope.Data.IntentID != intentID) || networkprotocol.ValidateVPNProbeBatch(*envelope.Data, time.Now().UTC()) != nil) {
		return nil, unavailable(fmt.Errorf("VPN probe response binding mismatch"))
	}
	return envelope.Data, nil
}

func (client *Client) VPNMetrics(ctx context.Context, query domain.VPNMetricsQuery) (domain.VPNMetrics, error) {
	raw, err := json.Marshal(query)
	if err != nil {
		return domain.VPNMetrics{}, err
	}
	var envelope struct {
		Data domain.VPNMetrics `json:"data"`
	}
	if err := client.vpnRequest(ctx, http.MethodPost, "/api/ingest/v1/query/vpn/metrics", raw, &envelope); err != nil {
		return domain.VPNMetrics{}, err
	}
	if err := validateVPNMetrics(envelope.Data, query); err != nil {
		return domain.VPNMetrics{}, unavailable(err)
	}
	return envelope.Data, nil
}

func (client *Client) vpnRequest(ctx context.Context, method, route string, body []byte, target any) error {
	request, err := http.NewRequestWithContext(ctx, method, client.origin+route, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return unavailable(fmt.Errorf("VPN query returned HTTP %d", response.StatusCode))
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return unavailable(fmt.Errorf("VPN query returned a non-JSON response"))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(raw)) > client.maxResponseBytes {
		return unavailable(fmt.Errorf("VPN query response exceeded limit"))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return unavailable(fmt.Errorf("VPN query response is invalid"))
	}
	return nil
}

func validateVPNMetrics(value domain.VPNMetrics, query domain.VPNMetricsQuery) error {
	if !value.From.Equal(query.From) || !value.To.Equal(query.To) || len(value.Sessions) > len(query.Sessions) || len(value.Series) > 169 || value.AsOf.IsZero() || value.AsOf.After(time.Now().Add(time.Minute)) {
		return fmt.Errorf("VPN metrics window or dimensions mismatch")
	}
	allowed := map[string]string{}
	gateways := map[string]bool{}
	for _, binding := range query.Sessions {
		allowed[binding.SessionID] = binding.GatewayID
		gateways[binding.GatewayID] = true
	}
	for _, binding := range query.Probes {
		for _, id := range binding.GatewayIDs {
			gateways[id] = true
		}
	}
	if err := validateVPNSessions(value.Sessions, query, allowed); err != nil {
		return err
	}
	if err := validateVPNGateways(value.Gateways, gateways); err != nil {
		return err
	}
	if err := validateVPNSeries(value.Series, query); err != nil {
		return err
	}

	if value.SampleCount < 0 || !validVPNMetric(value.LatencyP50Ms) || !validVPNMetric(value.LatencyP95Ms) {
		return fmt.Errorf("VPN metric quantiles are invalid")
	}
	return nil
}

func validVPNMetric(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0)
}

func validateVPNSessions(items []domain.VPNSessionMetrics, query domain.VPNMetricsQuery, allowed map[string]string) error {
	seen := map[string]bool{}
	for _, item := range items {
		if item.GatewayID == "" || allowed[item.SessionID] != item.GatewayID || item.MeasuredAt.Before(query.From) || !item.MeasuredAt.Before(query.To) || seen[item.SessionID] || item.UploadBytes < 0 || item.DownloadBytes < 0 || !validVPNMetric(item.UploadBytesPerSecond) || !validVPNMetric(item.DownloadBytesPerSecond) {
			return fmt.Errorf("VPN metric session is outside the requested scope")
		}
		seen[item.SessionID] = true
	}
	return nil
}

func validateVPNGateways(items []domain.VPNGatewayMetrics, gateways map[string]bool) error {
	seenGateways := map[string]bool{}
	for _, item := range items {
		if seenGateways[item.GatewayID] {
			return fmt.Errorf("duplicate VPN metric gateway")
		}
		seenGateways[item.GatewayID] = true
		if !gateways[item.GatewayID] || item.UploadBytes < 0 || item.DownloadBytes < 0 || item.SampleCount < 0 || !validVPNMetric(item.LatencyP50Ms) || !validVPNMetric(item.LatencyP95Ms) {
			return fmt.Errorf("VPN metric gateway is outside the requested scope")
		}
	}
	return nil
}

func validateVPNSeries(items []domain.VPNMetricPoint, query domain.VPNMetricsQuery) error {
	var previous time.Time
	for _, item := range items {
		if item.At.Before(query.From.UTC().Truncate(time.Hour)) || !item.At.Before(query.To) || !item.At.After(previous) || item.UploadBytes < 0 || item.DownloadBytes < 0 || !validVPNMetric(item.LatencyP50Ms) || !validVPNMetric(item.LatencyP95Ms) {
			return fmt.Errorf("invalid VPN metric series")
		}
		previous = item.At
	}
	return nil
}
