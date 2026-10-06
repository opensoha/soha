package networkingestquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	domainnetworkproxy "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Client struct {
	origin           string
	http             *http.Client
	maxResponseBytes int64
}

func New(origin string, httpClient *http.Client, maxResponseBytes int64) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" || httpClient == nil || maxResponseBytes < 1024 || maxResponseBytes > 4<<20 {
		return nil, fmt.Errorf("network ingest query client configuration is invalid")
	}
	copy := *httpClient
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{origin: strings.TrimRight(parsed.String(), "/"), http: &copy, maxResponseBytes: maxResponseBytes}, nil
}

func (client *Client) Summary(ctx context.Context, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	query := url.Values{}
	if !filter.From.IsZero() {
		query.Set("from", filter.From.UTC().Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		query.Set("to", filter.To.UTC().Format(time.RFC3339Nano))
	}
	if filter.ProducerID != "" {
		query.Set("producerId", filter.ProducerID)
	}
	if filter.Limit != 0 {
		query.Set("limit", strconv.Itoa(filter.Limit))
	}
	endpoint := client.origin + "/api/ingest/v1/query/summary"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domainnetworkingest.Summary{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return domainnetworkingest.Summary{}, unavailable(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return domainnetworkingest.Summary{}, unavailable(fmt.Errorf("ingest returned HTTP %d", response.StatusCode))
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return domainnetworkingest.Summary{}, unavailable(fmt.Errorf("ingest returned a non-JSON response"))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(raw)) > client.maxResponseBytes {
		return domainnetworkingest.Summary{}, unavailable(fmt.Errorf("ingest aggregate response exceeds the configured limit"))
	}
	var envelope struct {
		Data domainnetworkingest.Summary `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF || validateSummary(envelope.Data) != nil {
		return domainnetworkingest.Summary{}, unavailable(fmt.Errorf("invalid aggregate response"))
	}
	return envelope.Data, nil
}

func (client *Client) Samples(ctx context.Context, instanceID string, from, to time.Time) ([]domainnetworkproxy.TrafficSample, error) {
	query := url.Values{"instanceId": {instanceID}, "from": {from.UTC().Format(time.RFC3339Nano)}, "to": {to.UTC().Format(time.RFC3339Nano)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.origin+"/api/ingest/v1/query/proxy-runtime/samples?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return nil, unavailable(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, unavailable(fmt.Errorf("ingest returned HTTP %d", response.StatusCode))
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, unavailable(fmt.Errorf("ingest returned a non-JSON response"))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(raw)) > client.maxResponseBytes {
		return nil, unavailable(fmt.Errorf("ingest sample response exceeds the configured limit"))
	}
	var envelope struct {
		Data domainnetworkingest.ProxyRuntimeSeries `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF || validateProxySamples(envelope.Data, instanceID, from, to) != nil {
		return nil, unavailable(fmt.Errorf("invalid proxy sample response"))
	}
	samples := make([]domainnetworkproxy.TrafficSample, 0, len(envelope.Data.Samples))
	for _, item := range envelope.Data.Samples {
		samples = append(samples, domainnetworkproxy.TrafficSample{ObservedAt: item.ObservedAt, UptimeSeconds: item.UptimeSeconds,
			UploadTotal: item.UploadTotal, DownloadTotal: item.DownloadTotal, ActiveConnections: item.ActiveConnections})
	}
	return samples, nil
}

func validateProxySamples(series domainnetworkingest.ProxyRuntimeSeries, instanceID string, from, to time.Time) error {
	if series.InstanceID != instanceID || series.Samples == nil || len(series.Samples) > 1440 {
		return fmt.Errorf("invalid proxy sample series")
	}
	var previous time.Time
	for _, sample := range series.Samples {
		if sample.ObservedAt.Before(from) || sample.ObservedAt.After(to) || !sample.ObservedAt.After(previous) ||
			sample.UptimeSeconds < 0 || sample.UploadTotal < 0 || sample.DownloadTotal < 0 ||
			(sample.ActiveConnections != nil && (*sample.ActiveConnections < 0 || *sample.ActiveConnections > 1000000)) {
			return fmt.Errorf("invalid proxy sample")
		}
		previous = sample.ObservedAt
	}
	return nil
}

func validateSummary(summary domainnetworkingest.Summary) error {
	if summary.From.IsZero() || !summary.To.After(summary.From) || summary.To.Sub(summary.From) > 7*24*time.Hour || len(summary.Producers) > 200 || len(summary.ProxyFlows) > 200 {
		return fmt.Errorf("invalid window")
	}
	if err := validateSummaryCounts(summary); err != nil {
		return err
	}
	for _, producer := range summary.Producers {
		if err := validateSummaryProducer(producer); err != nil {
			return err
		}
	}
	for _, flow := range summary.ProxyFlows {
		if err := validateSummaryProxyFlow(summary, flow); err != nil {
			return err
		}
	}
	return nil
}

func validateSummaryCounts(summary domainnetworkingest.Summary) error {
	counts := []int64{summary.EventCount, summary.HeartbeatCount, summary.RadiusAccountingCount, summary.NetworkFlowCount, summary.ConnectionSummaryCount, summary.ProxyFlowCount, summary.UploadBytes, summary.DownloadBytes, summary.ActiveConnections}
	for _, value := range counts {
		if value < 0 {
			return fmt.Errorf("negative aggregate")
		}
	}
	if summary.EventCount != summary.HeartbeatCount+summary.RadiusAccountingCount+summary.NetworkFlowCount+summary.ConnectionSummaryCount+summary.ProxyFlowCount {
		return fmt.Errorf("inconsistent event counts")
	}
	return nil
}

func validateSummaryProducer(producer domainnetworkingest.ProducerSummary) error {
	if !validText(producer.ProducerID, 128) || !oneOf(producer.ProducerKind, "endpoint", "gateway", "freeradius", "network-control") {
		return fmt.Errorf("invalid producer")
	}
	if producer.LastSeenAt.IsZero() || producer.GapCount < 0 || producer.RegressionCount < 0 {
		return fmt.Errorf("invalid producer")
	}
	return nil
}

func validateSummaryProxyFlow(summary domainnetworkingest.Summary, flow domainnetworkingest.ProxyFlowSummary) error {
	if !validText(flow.ProducerID, 128) || flow.Engine != "mihomo" || !validText(flow.ProfileID, 128) {
		return fmt.Errorf("invalid proxy flow")
	}
	if flow.ProfileRevision < 1 || !oneOf(flow.Mode, "managed_follow", "app_subscription") || !validText(flow.SelectedProxy, 128) {
		return fmt.Errorf("invalid proxy flow")
	}
	if flow.UploadBytes < 0 || flow.DownloadBytes < 0 || flow.ActiveConnections < 0 || flow.ActiveConnections > 1_000_000 {
		return fmt.Errorf("invalid proxy flow")
	}
	if flow.LastOccurredAt.Before(summary.From) || !flow.LastOccurredAt.Before(summary.To) {
		return fmt.Errorf("invalid proxy flow")
	}
	return nil
}

func validText(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func unavailable(err error) error {
	return apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "network_ingest_unavailable", "Network telemetry is temporarily unavailable.", "网络遥测暂时不可用。")
}
