package networkproxyruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func (c *Client) PutSnapshot(ctx context.Context, engine string, sample domain.Observation, connections []domain.Connection) error {
	return c.post(ctx, "/connections", "connections.snapshot", map[string]any{
		"engine": engine, "connections": connections, "processEpoch": sample.ProcessEpoch,
		"sampleSequence": sample.SampleSequence, "totalActive": sample.ActiveConnections,
		"returnedCount": len(connections), "truncated": sample.ActiveConnections != nil && *sample.ActiveConnections > len(connections), "freshness": "fresh",
	})
}

func (c *Client) NextCommand(ctx context.Context, epoch string) (*api.NetworkProxyRuntimeCommandInput, error) {
	raw, err := c.read(ctx, "/commands/next?processEpoch="+url.QueryEscape(epoch), "command.request")
	if err != nil || raw == nil {
		return nil, err
	}
	var command api.NetworkProxyRuntimeCommandInput
	if err := json.Unmarshal(raw, &command); err != nil {
		return nil, err
	}
	return &command, nil
}

func (c *Client) CompleteCommand(ctx context.Context, receipt api.NetworkProxyRuntimeCommandReceipt) error {
	return c.post(ctx, "/commands/"+url.PathEscape(receipt.RequestID)+"/result", "command.result", receipt)
}

// These endpoints carry SDK DTOs, not the runtime envelope. Failures never echo
// upstream bodies: ingress requests contain employee credentials.
func (c *Client) typed(ctx context.Context, method, path string, input, output any) error {
	status, raw, err := c.request(ctx, method, c.origin+path, input, "")
	if err != nil {
		return err
	}
	if status == http.StatusNoContent && output == nil {
		return nil
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return operationStatusError(status)
	}
	if output == nil {
		return nil
	}
	// Core's standard success response wraps data; raw typed fixtures remain valid.
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Data) > 0 {
		raw = envelope.Data
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return &ingressCheckError{message: "invalid proxy operation response", classification: "response_invalid", status: status}
	}
	return nil
}

func (c *Client) CheckIngress(ctx context.Context, input api.NetworkProxyIngressCheckInput) (api.NetworkProxyIngressCheckResult, error) {
	var result api.NetworkProxyIngressCheckResult
	trace := newIngressHTTPTrace()
	ctx = trace.context(ctx)
	err := c.typed(ctx, http.MethodPost, "/api/network-control/v1/proxies/"+url.PathEscape(c.runtimeID)+"/ingress/check", input, &result)
	if err != nil {
		return result, trace.wrap(err)
	}
	if !result.Allowed {
		return result, trace.wrap(ingressDecisionError("denied", "proxy ingress authorization denied", result))
	}
	if result.BundleID != input.BundleID || result.NodeID != input.NodeID || result.Revision != input.Revision || result.CredentialEpoch != input.CredentialEpoch {
		return result, trace.wrap(ingressDecisionError("identity_mismatch", "proxy ingress authorization denied", result))
	}
	if !result.ExpiresAt.After(time.Now()) {
		return result, trace.wrap(ingressDecisionError("decision_expired", "proxy ingress authorization denied", result))
	}
	return result, nil
}

func (c *Client) PublicationReady(ctx context.Context, readback api.NetworkProxyBundleReadback) error {
	return c.typed(ctx, http.MethodPost, "/api/network-control/v1/proxies/"+url.PathEscape(c.runtimeID)+"/publication/ready", readback, nil)
}
