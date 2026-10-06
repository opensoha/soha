package networkproxyruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type Client struct {
	origin    string
	runtimeID string
	http      *http.Client
	schemas   *networkprotocol.Schemas
}

type controlStatusError int

func (status controlStatusError) Error() string {
	return fmt.Sprintf("control returned HTTP %d", status)
}

func NewClient(origin, runtimeID string, tlsConfig *tls.Config) *Client {
	transport := cloneDefaultTransport()
	transport.TLSClientConfig = tlsConfig
	return &Client{origin: strings.TrimRight(origin, "/"), runtimeID: runtimeID,
		http: &http.Client{Transport: transport, Timeout: 12 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func cloneDefaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment}
}

func (c *Client) SetSchemas(schemas *networkprotocol.Schemas) { c.schemas = schemas }

func (c *Client) enrollmentIdentity() (string, string, error) {
	transport, ok := c.http.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || len(transport.TLSClientConfig.Certificates) == 0 || len(transport.TLSClientConfig.Certificates[0].Certificate) == 0 {
		return "", "", fmt.Errorf("proxy control TLS certificate is required")
	}
	certificate, err := x509.ParseCertificate(transport.TLSClientConfig.Certificates[0].Certificate[0])
	if err != nil {
		return "", "", err
	}
	identity, err := networkidentity.ParseCertificate(certificate, networkidentity.ScopeNetworkControl)
	if err != nil {
		return "", "", err
	}
	if identity.Kind != "proxy" || identity.ID != c.runtimeID {
		return "", "", fmt.Errorf("control certificate does not match the proxy runtime")
	}
	publicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKey})), identity.CertificateFingerprint, nil
}

func (c *Client) path(suffix string) string {
	return c.origin + "/api/network-control/v1/proxy-instances/" + url.PathEscape(c.runtimeID) + suffix
}

func (c *Client) request(ctx context.Context, method, endpoint string, body any, token string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return response.StatusCode, nil, err
	}
	if len(raw) >= 2<<20 {
		return response.StatusCode, nil, fmt.Errorf("control response is too large")
	}
	if response.StatusCode >= 400 {
		return response.StatusCode, nil, controlStatusError(response.StatusCode)
	}
	return response.StatusCode, raw, nil
}

func (c *Client) Enroll(ctx context.Context, enrollmentID, challengeID, publicKey, token, version string) error {
	if enrollmentID == "" || challengeID == "" || publicKey == "" || token == "" {
		return fmt.Errorf("proxy enrollment is incomplete")
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(networkprotocol.EnrollmentRequest{EnrollmentID: enrollmentID, ChallengeID: challengeID,
		DeviceID: c.runtimeID, DevicePublicKey: publicKey, Platform: "linux", ClientVersion: version,
		Capabilities: []string{"proxy-runtime-v1"}})
	if err != nil {
		return err
	}
	message := networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(),
		MessageType: networkprotocol.MessageEnrollmentRequest, ProducerID: c.runtimeID, RuntimeID: c.runtimeID,
		RuntimeKind: "proxy", OccurredAt: now, ExpiresAt: now.Add(time.Minute), Payload: payload}
	status, raw, err := c.request(ctx, http.MethodPost, c.origin+"/api/network-control/v1/runtimes/"+url.PathEscape(c.runtimeID)+"/enroll", message, token)
	if err != nil {
		return err
	}
	if status != http.StatusCreated {
		return fmt.Errorf("proxy enrollment returned HTTP %d", status)
	}
	if c.schemas == nil {
		return fmt.Errorf("runtime schemas are required")
	}
	if err := c.schemas.ValidateRuntime(raw); err != nil {
		return fmt.Errorf("invalid enrollment response: %w", err)
	}
	var result networkprotocol.RuntimeMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.MessageType != networkprotocol.MessageEnrollmentResult || result.RuntimeID != c.runtimeID || result.RuntimeKind != "proxy" {
		return fmt.Errorf("enrollment response identity is invalid")
	}
	accepted, err := networkprotocol.DecodePayload[networkprotocol.EnrollmentResult](result.Payload)
	if err != nil || !accepted.Accepted {
		return fmt.Errorf("proxy enrollment was rejected")
	}
	return nil
}

type proxyMessage struct {
	SchemaVersion string          `json:"schemaVersion"`
	RuntimeID     string          `json:"runtimeId"`
	MessageType   string          `json:"messageType"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Payload       json.RawMessage `json:"payload"`
}

func (c *Client) message(kind string, payload any) proxyMessage {
	raw, _ := json.Marshal(payload)
	return proxyMessage{SchemaVersion: "proxy-runtime/v1alpha1", RuntimeID: c.runtimeID, MessageType: kind,
		OccurredAt: time.Now().UTC(), Payload: raw}
}

func (c *Client) post(ctx context.Context, path, kind string, payload any) error {
	message := c.message(kind, payload)
	if c.schemas == nil {
		return fmt.Errorf("runtime schemas are required")
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := c.schemas.ValidateProxyRuntime(raw); err != nil {
		return fmt.Errorf("proxy message is invalid: %w", err)
	}
	status, _, err := c.request(ctx, http.MethodPost, c.path(path), message, "")
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("proxy control returned HTTP %d", status)
	}
	return nil
}

func (c *Client) read(ctx context.Context, path, kind string) (json.RawMessage, error) {
	status, raw, err := c.request(ctx, http.MethodGet, c.path(path), nil, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	if status != http.StatusOK || c.schemas == nil {
		return nil, fmt.Errorf("proxy control response is invalid")
	}
	if err := c.schemas.ValidateProxyRuntime(raw); err != nil {
		return nil, fmt.Errorf("proxy control response violates contract: %w", err)
	}
	var message proxyMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	if message.RuntimeID != c.runtimeID || message.MessageType != kind {
		return nil, fmt.Errorf("proxy control response identity is invalid")
	}
	if message.OccurredAt.After(time.Now().Add(time.Minute)) || message.OccurredAt.Before(time.Now().Add(-2*time.Minute)) {
		return nil, fmt.Errorf("proxy control response is stale")
	}
	return message.Payload, nil
}

func (c *Client) Configuration(ctx context.Context) (*domain.Configuration, error) {
	raw, err := c.read(ctx, "/configuration", "configuration.desired")
	if err != nil || raw == nil {
		return nil, err
	}
	var desired domain.Configuration
	if err := json.Unmarshal(raw, &desired); err != nil {
		return nil, err
	}
	if !desired.ValidUntil.After(time.Now()) {
		return nil, fmt.Errorf("proxy configuration expired")
	}
	digest := sha256.Sum256([]byte(desired.Content))
	if desired.ContentHash != fmt.Sprintf("sha256:%x", digest) {
		return nil, fmt.Errorf("proxy configuration hash mismatch")
	}
	return &desired, nil
}

func (c *Client) Applied(ctx context.Context, applied domain.Applied) error {
	return c.post(ctx, "/configuration:applied", "configuration.applied", applied)
}

func (c *Client) Observe(ctx context.Context, observation domain.Observation) error {
	return c.post(ctx, "/observations", "observation", observation)
}

func (c *Client) PutConnections(ctx context.Context, engine string, connections []domain.Connection) error {
	return c.post(ctx, "/connections", "connections.snapshot", map[string]any{"engine": engine, "connections": connections})
}

func (c *Client) NextClose(ctx context.Context) (*domain.CloseCommand, error) {
	raw, err := c.read(ctx, "/close-commands/next", "connection.close.request")
	if err != nil || raw == nil {
		return nil, err
	}
	var command domain.CloseCommand
	if err := json.Unmarshal(raw, &command); err != nil {
		return nil, err
	}
	return &command, nil
}

func (c *Client) CompleteClose(ctx context.Context, command domain.CloseCommand) error {
	payload := map[string]any{"commandId": command.ID, "status": command.Status}
	if command.ReasonCode != "" {
		payload["reasonCode"] = command.ReasonCode
	}
	return c.post(ctx, "/close-commands/"+url.PathEscape(command.ID)+"/result", "connection.close.result", payload)
}
