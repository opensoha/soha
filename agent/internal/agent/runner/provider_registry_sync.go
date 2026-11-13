package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

const maxProviderRegistryResponseBytes = 4 << 20

type providerRegistryEnvelope struct {
	Data AgentProviderRegistry `json:"data"`
}

type providerRegistryAcknowledgement struct {
	RunnerID          string                           `json:"runnerId"`
	Revision          uint64                           `json:"revision"`
	ActiveRevision    uint64                           `json:"activeRevision"`
	Accepted          bool                             `json:"accepted"`
	Targeted          bool                             `json:"targeted"`
	DesiredRevision   uint64                           `json:"desiredRevision"`
	LKGRevision       uint64                           `json:"lkgRevision"`
	PreviousRevision  uint64                           `json:"previousRevision,omitempty"`
	RolloutState      string                           `json:"rolloutState"`
	RolledBack        bool                             `json:"rolledBack,omitempty"`
	ConformanceChecks []AgentProviderConformanceResult `json:"conformanceChecks,omitempty"`
	Reason            string                           `json:"reason,omitempty"`
	ObservedAt        time.Time                        `json:"observedAt"`
	ProviderStatuses  []AgentProviderRuntimeStatus     `json:"providerStatuses,omitempty"`
}

func (r *Runner) agentProviderRegistryLoop(ctx context.Context) {
	interval := r.cfg.AgentRuntime.PollInterval
	if interval <= 0 {
		interval = r.cfg.PollInterval
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	r.syncAgentProviderRegistry(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncAgentProviderRegistry(ctx)
		}
	}
}

func (r *Runner) syncAgentProviderRegistry(ctx context.Context) {
	runnerID := firstNonEmpty(r.cfg.AgentRuntime.WorkerID, r.cfg.AgentID, "local-agent")
	snapshot, err := r.fetchAgentProviderRegistry(ctx, runnerID)
	if err != nil {
		r.logger.Warn("agent provider registry fetch failed", zap.Error(err))
		return
	}
	result := r.applyAgentProviderRegistrySnapshot(ctx, snapshot, time.Now().UTC())
	r.refreshProviderHealth(ctx)
	ack := providerRegistryAcknowledgement{
		RunnerID:          runnerID,
		Revision:          result.Revision,
		ActiveRevision:    result.ActiveRevision,
		Accepted:          result.Accepted,
		Targeted:          result.Targeted,
		DesiredRevision:   result.DesiredRevision,
		LKGRevision:       result.LKGRevision,
		PreviousRevision:  result.PreviousRevision,
		RolloutState:      result.RolloutState,
		RolledBack:        result.RolledBack,
		ConformanceChecks: result.ConformanceChecks,
		Reason:            result.Reason,
		ObservedAt:        result.ObservedAt,
		ProviderStatuses:  r.AgentProviderRuntimeStatuses(),
	}
	if err := r.sendAgentProviderRegistryAck(ctx, ack); err != nil {
		r.logger.Warn("agent provider registry acknowledgement failed", zap.Error(err))
	}
}

func (r *Runner) fetchAgentProviderRegistry(ctx context.Context, runnerID string) (AgentProviderRegistry, error) {
	endpoint, err := providerRegistryEndpoint(r.cfg.BaseURL, "/ai/agent-providers/registry-snapshot")
	if err != nil {
		return AgentProviderRegistry{}, err
	}
	query := endpoint.Query()
	query.Set("runnerId", runnerID)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return AgentProviderRegistry{}, fmt.Errorf("build provider registry request: %w", err)
	}
	r.authorizeProviderRegistryRequest(req)
	var envelope providerRegistryEnvelope
	if err := r.doProviderRegistryRequest(req, &envelope); err != nil {
		return AgentProviderRegistry{}, err
	}
	return envelope.Data, nil
}

func (r *Runner) sendAgentProviderRegistryAck(ctx context.Context, ack providerRegistryAcknowledgement) error {
	endpoint, err := providerRegistryEndpoint(r.cfg.BaseURL, "/ai/agent-providers/registry-acks")
	if err != nil {
		return err
	}
	body, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("encode provider registry acknowledgement: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build provider registry acknowledgement request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	r.authorizeProviderRegistryRequest(req)
	return r.doProviderRegistryRequest(req, nil)
}

func (r *Runner) authorizeProviderRegistryRequest(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(r.cfg.BearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func (r *Runner) doProviderRegistryRequest(req *http.Request, output any) error {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform provider registry request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProviderRegistryResponseBytes))
		return fmt.Errorf("provider registry request returned HTTP %d", resp.StatusCode)
	}
	if output == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProviderRegistryResponseBytes))
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxProviderRegistryResponseBytes+1))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode provider registry response: %w", err)
	}
	return nil
}

func providerRegistryEndpoint(baseURL, path string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("control plane base URL is invalid")
	}
	base.Path = strings.TrimSuffix(strings.TrimRight(base.Path, "/"), "/api/v1") + "/api/v1/" + strings.TrimLeft(path, "/")
	base.RawQuery = ""
	base.Fragment = ""
	return base, nil
}
