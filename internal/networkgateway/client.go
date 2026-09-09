package networkgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const maxRuntimeResponseBytes = 1 << 20

type Enrollment struct {
	EnrollmentID       string
	ChallengeID        string
	DeviceID           string
	DevicePublicKey    string
	WireGuardPublicKey string
	ClientVersion      string
	Token              string
}

type ControlClient struct {
	origin       string
	runtimeID    string
	http         *http.Client
	schemas      *networkprotocol.Schemas
	maxClockSkew time.Duration
	now          func() time.Time
}

func NewControlClient(origin, runtimeID string, client *http.Client, schemas *networkprotocol.Schemas, maxClockSkew time.Duration) (*ControlClient, error) {
	parsed, err := runtimeOrigin(origin)
	if err != nil || !identifierPattern.MatchString(runtimeID) || client == nil || schemas == nil || maxClockSkew <= 0 || maxClockSkew > 5*time.Minute {
		return nil, fmt.Errorf("network control client configuration is invalid")
	}
	return &ControlClient{origin: parsed, runtimeID: runtimeID, http: client, schemas: schemas, maxClockSkew: maxClockSkew, now: time.Now}, nil
}

func (c *ControlClient) Configuration(ctx context.Context) (networkprotocol.RuntimeMessage, error) {
	endpoint := c.origin + "/api/network-control/v1/runtimes/" + url.PathEscape(c.runtimeID) + "/configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, fmt.Errorf("get network gateway configuration: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := readRuntimeResponse(response, http.StatusOK)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if err := c.schemas.ValidateRuntime(raw); err != nil {
		return networkprotocol.RuntimeMessage{}, fmt.Errorf("validate desired configuration: %w", err)
	}
	message, err := networkprotocol.DecodeRuntimeMessage(raw)
	if err != nil || message.MessageType != networkprotocol.MessageConfiguration || message.ProducerID != "network-control" || message.RuntimeID != c.runtimeID || message.RuntimeKind != "gateway" {
		return networkprotocol.RuntimeMessage{}, fmt.Errorf("network control returned an invalid gateway configuration message")
	}
	if err := networkprotocol.ValidateRuntimeMessageWindow(message, c.now().UTC(), c.maxClockSkew); err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return message, nil
}

func (c *ControlClient) Report(ctx context.Context, applied networkprotocol.ConfigurationApplied) error {
	_, raw, err := c.runtimeMessage(networkprotocol.MessageConfigurationApply, applied)
	if err != nil {
		return err
	}
	endpoint := c.origin + "/api/network-control/v1/runtimes/" + url.PathEscape(c.runtimeID) + "/configuration:applied"
	return c.post(ctx, endpoint, raw, "", http.StatusNoContent)
}

func (c *ControlClient) Enroll(ctx context.Context, enrollment Enrollment) (networkprotocol.EnrollmentResult, error) {
	if err := validateGatewayEnrollment(enrollment); err != nil {
		return networkprotocol.EnrollmentResult{}, err
	}
	payload := networkprotocol.EnrollmentRequest{EnrollmentID: enrollment.EnrollmentID, ChallengeID: enrollment.ChallengeID, DeviceID: enrollment.DeviceID, DevicePublicKey: enrollment.DevicePublicKey, WireGuardPublicKey: enrollment.WireGuardPublicKey, Platform: "linux", ClientVersion: enrollment.ClientVersion, Capabilities: []string{"wireguard"}}
	_, raw, err := c.runtimeMessage(networkprotocol.MessageEnrollmentRequest, payload)
	if err != nil {
		return networkprotocol.EnrollmentResult{}, err
	}
	endpoint := c.origin + "/api/network-control/v1/runtimes/" + url.PathEscape(c.runtimeID) + "/enroll"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return networkprotocol.EnrollmentResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+enrollment.Token)
	response, err := c.http.Do(request)
	if err != nil {
		return networkprotocol.EnrollmentResult{}, fmt.Errorf("enroll network gateway: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseRaw, err := readRuntimeResponse(response, http.StatusCreated)
	if err != nil {
		return networkprotocol.EnrollmentResult{}, err
	}
	if err := c.schemas.ValidateRuntime(responseRaw); err != nil {
		return networkprotocol.EnrollmentResult{}, fmt.Errorf("validate enrollment result: %w", err)
	}
	return c.decodeEnrollmentResult(responseRaw)
}

func validateGatewayEnrollment(enrollment Enrollment) error {
	if !identifierPattern.MatchString(enrollment.EnrollmentID) || !identifierPattern.MatchString(enrollment.ChallengeID) || !identifierPattern.MatchString(enrollment.DeviceID) {
		return fmt.Errorf("gateway enrollment parameters are invalid")
	}
	if len(enrollment.DevicePublicKey) > 8192 || strings.TrimSpace(enrollment.DevicePublicKey) == "" || len(enrollment.ClientVersion) == 0 || len(enrollment.ClientVersion) > 128 {
		return fmt.Errorf("gateway enrollment parameters are invalid")
	}
	if len(enrollment.Token) < 32 || len(enrollment.Token) > 4096 || strings.TrimSpace(enrollment.Token) != enrollment.Token {
		return fmt.Errorf("gateway enrollment parameters are invalid")
	}
	if _, err := wgtypes.ParseKey(enrollment.WireGuardPublicKey); err != nil {
		return fmt.Errorf("gateway WireGuard public key is invalid")
	}
	return nil
}

func (c *ControlClient) decodeEnrollmentResult(responseRaw []byte) (networkprotocol.EnrollmentResult, error) {
	message, err := networkprotocol.DecodeRuntimeMessage(responseRaw)
	if err != nil || message.MessageType != networkprotocol.MessageEnrollmentResult || message.ProducerID != "network-control" || message.RuntimeID != c.runtimeID || message.RuntimeKind != "gateway" {
		return networkprotocol.EnrollmentResult{}, fmt.Errorf("network control returned an invalid enrollment result")
	}
	if err := networkprotocol.ValidateRuntimeMessageWindow(message, c.now().UTC(), c.maxClockSkew); err != nil {
		return networkprotocol.EnrollmentResult{}, err
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.EnrollmentResult](message.Payload)
	if err != nil || !result.Accepted {
		return networkprotocol.EnrollmentResult{}, fmt.Errorf("network gateway enrollment was not accepted")
	}
	return result, nil
}

func (c *ControlClient) runtimeMessage(messageType string, payload any) (networkprotocol.RuntimeMessage, []byte, error) {
	now := c.now().UTC()
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, nil, err
	}
	message := networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(), MessageType: messageType, ProducerID: c.runtimeID, RuntimeID: c.runtimeID, RuntimeKind: "gateway", OccurredAt: now, ExpiresAt: now.Add(min(c.maxClockSkew, 2*time.Minute)), Payload: payloadRaw}
	raw, err := json.Marshal(message)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, nil, err
	}
	if err := c.schemas.ValidateRuntime(raw); err != nil {
		return networkprotocol.RuntimeMessage{}, nil, fmt.Errorf("gateway runtime message violates contract: %w", err)
	}
	return message, raw, nil
}

func (c *ControlClient) post(ctx context.Context, endpoint string, raw []byte, authorization string, expectedStatus int) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != expectedStatus {
		return fmt.Errorf("network control returned HTTP %d", response.StatusCode)
	}
	return nil
}

type TelemetryClient struct {
	origin    string
	runtimeID string
	http      *http.Client
	schemas   *networkprotocol.Schemas
	sequence  atomic.Int64
	now       func() time.Time
}

func NewTelemetryClient(origin, runtimeID string, client *http.Client, schemas *networkprotocol.Schemas) (*TelemetryClient, error) {
	parsed, err := runtimeOrigin(origin)
	if err != nil || !identifierPattern.MatchString(runtimeID) || client == nil || schemas == nil {
		return nil, fmt.Errorf("network ingest client configuration is invalid")
	}
	return &TelemetryClient{origin: parsed, runtimeID: runtimeID, http: client, schemas: schemas, now: time.Now}, nil
}

func (c *TelemetryClient) Heartbeat(ctx context.Context, status RuntimeStatus) error {
	now := c.now().UTC()
	payload, err := json.Marshal(struct {
		Status               string   `json:"status"`
		ConfigurationVersion int      `json:"configurationVersion"`
		PolicyVersion        int      `json:"policyVersion"`
		UptimeSeconds        int64    `json:"uptimeSeconds"`
		Diagnostics          []string `json:"diagnostics,omitempty"`
	}{status.Status, status.ConfigurationVersion, status.PolicyVersion, status.UptimeSeconds, status.Diagnostics})
	if err != nil {
		return err
	}
	batch := networkprotocol.IngestBatch{SchemaVersion: networkprotocol.IngestSchemaVersion, BatchID: uuid.NewString(), ProducerID: c.runtimeID, ProducerKind: "gateway", SentAt: now, Events: []networkprotocol.IngestEvent{{ID: uuid.NewString(), Type: networkprotocol.EventHeartbeat, Sequence: c.sequence.Add(1), OccurredAt: now, Payload: payload}}}
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if err := c.schemas.ValidateIngest(raw); err != nil {
		return fmt.Errorf("gateway heartbeat violates contract: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+"/api/ingest/v1/events:batch", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("send network gateway heartbeat: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("network ingest returned HTTP %d", response.StatusCode)
	}
	return nil
}

func runtimeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("runtime endpoint must be an HTTPS origin")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func readRuntimeResponse(response *http.Response, expectedStatus int) ([]byte, error) {
	if response.StatusCode != expectedStatus {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("network control returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, fmt.Errorf("network control returned a non-JSON response")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxRuntimeResponseBytes+1))
	if err != nil || len(raw) > maxRuntimeResponseBytes {
		return nil, fmt.Errorf("network control response exceeded the safe limit")
	}
	return raw, nil
}
