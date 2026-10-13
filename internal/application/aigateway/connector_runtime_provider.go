package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const connectorRuntimeRequestTimeout = 10 * time.Second

type ConnectorRuntimeOption func(*connectorRuntimeProvider)

type connectorRuntimeProvider struct {
	endpoint     string
	httpClient   *http.Client
	runtimeToken string
	pluginID     string
	connectorID  string
	actions      []connectorRuntimeAction
}

type connectorRuntimeManifest struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Actions     []connectorRuntimeAction `json:"actions"`
}

type connectorRuntimeAction struct {
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	InputSchema        map[string]any `json:"inputSchema"`
	CredentialProtocol string         `json:"credentialProtocol"`
	CredentialAliases  []string       `json:"credentialAliases"`
}

type connectorRuntimeActionResponse struct {
	OK     bool `json:"ok"`
	Output any  `json:"output"`
	Error  any  `json:"error"`
}

func WithConnectorRuntimeToken(token string) ConnectorRuntimeOption {
	return func(provider *connectorRuntimeProvider) {
		provider.runtimeToken = strings.TrimSpace(token)
	}
}

func WithConnectorRuntimePluginID(pluginID string) ConnectorRuntimeOption {
	return func(provider *connectorRuntimeProvider) {
		provider.pluginID = strings.TrimSpace(pluginID)
	}
}

func WithConnectorRuntimeConnectorID(connectorID string) ConnectorRuntimeOption {
	return func(provider *connectorRuntimeProvider) {
		provider.connectorID = strings.TrimSpace(connectorID)
	}
}

func DiscoverConnectorRuntime(ctx context.Context, endpoint string, client *http.Client, options ...ConnectorRuntimeOption) (CapabilityProvider, error) {
	provider := &connectorRuntimeProvider{
		endpoint:   strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		httpClient: client,
	}
	if provider.httpClient == nil {
		provider.httpClient = http.DefaultClient
	}
	for _, option := range options {
		if option != nil {
			option(provider)
		}
	}
	if provider.endpoint == "" {
		return nil, fmt.Errorf("%w: connector runtime endpoint is required", apperrors.ErrInvalidArgument)
	}
	if _, err := url.ParseRequestURI(provider.endpoint); err != nil {
		return nil, fmt.Errorf("%w: connector runtime endpoint is invalid", apperrors.ErrInvalidArgument)
	}

	requestContext, cancel := context.WithTimeout(ctx, connectorRuntimeRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, provider.endpoint+"/manifest", nil)
	if err != nil {
		return nil, err
	}
	if provider.runtimeToken != "" {
		req.Header.Set("Authorization", "Bearer "+provider.runtimeToken)
	}
	resp, err := provider.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if closeErr := resp.Body.Close(); closeErr != nil {
			return nil, fmt.Errorf("close connector runtime manifest response: %w", closeErr)
		}
		return nil, fmt.Errorf("%w: connector runtime manifest returned %d: %s", apperrors.ErrInvalidArgument, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var manifest connectorRuntimeManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: invalid connector runtime manifest: %v", apperrors.ErrInvalidArgument, errors.Join(err, resp.Body.Close()))
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("close connector runtime manifest response: %w", err)
	}
	provider.connectorID = firstNonEmpty(provider.connectorID, manifest.ID)
	if provider.pluginID == "" && provider.connectorID != "" {
		provider.pluginID = "opensoha." + provider.connectorID
	}
	for _, action := range manifest.Actions {
		action.Name = strings.TrimSpace(action.Name)
		if action.Name == "" {
			continue
		}
		provider.actions = append(provider.actions, action)
	}
	if len(provider.actions) == 0 {
		return nil, fmt.Errorf("%w: connector runtime manifest has no actions", apperrors.ErrInvalidArgument)
	}
	return provider, nil
}

func (p *connectorRuntimeProvider) Tools() []domainaigateway.ToolCapability {
	out := make([]domainaigateway.ToolCapability, 0, len(p.actions))
	for _, action := range p.actions {
		out = append(out, domainaigateway.ToolCapability{
			Name:           action.Name,
			Title:          action.Name,
			Description:    action.Description,
			Domain:         "connector",
			Action:         action.Name,
			RiskLevel:      domainaigateway.RiskLevelMutate,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			InputSchema:    cloneMap(action.InputSchema),
		})
	}
	return out
}

func (p *connectorRuntimeProvider) Resources() []domainaigateway.ResourceCapability {
	return nil
}

func (p *connectorRuntimeProvider) Prompts() []domainaigateway.PromptCapability {
	return nil
}

func (p *connectorRuntimeProvider) Skills() []domainaigateway.SkillCapability {
	return nil
}

func (p *connectorRuntimeProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if !providerHasTool(p, tool.Name) {
		return nil, nil, fmt.Errorf("%w: connector tool %s is not available", apperrors.ErrInvalidArgument, tool.Name)
	}
	requestContext, cancel := context.WithTimeout(ctx, connectorRuntimeRequestTimeout)
	defer cancel()
	envelope, needles, err := p.actionBody(requestContext, principal, tool, input)
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: invalid connector action input", apperrors.ErrInvalidArgument)
	}
	if len(needles) > 0 && len(body) > 1<<20 {
		return nil, nil, fmt.Errorf("%w: credential connector envelope exceeds 1 MiB", apperrors.ErrInvalidArgument)
	}
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, p.endpoint+"/actions/"+url.PathEscape(tool.Name), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.runtimeToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.runtimeToken)
	}
	client := *p.httpClient
	related := p.relatedIDs(tool.Name)
	if len(needles) > 0 {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		masked, ok := redactConnectorCredentialOutput(related, needles).(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("%w: invalid protected connector metadata", apperrors.ErrInvalidArgument)
		}
		related = masked
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, related, connectorActionTransportError(requestContext, err, needles)
	}
	output, err := connectorRuntimeActionOutput(requestContext, resp, needles)
	return output, related, err
}

func connectorRuntimeActionOutput(ctx context.Context, resp *http.Response, needles []string) (any, error) {
	payload, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, connectorActionTransportError(ctx, errors.Join(err, resp.Body.Close()), needles)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, connectorActionTransportError(ctx, fmt.Errorf("close connector action response: %w", err), needles)
	}
	if len(payload) > 1<<20 {
		return nil, fmt.Errorf("%w: connector action response exceeds 1 MiB", apperrors.ErrInvalidArgument)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(needles) > 0 {
			return nil, fmt.Errorf("%w: credential connector action returned %d", apperrors.ErrInvalidArgument, resp.StatusCode)
		}
		return nil, fmt.Errorf("%w: connector action returned %d: %s", apperrors.ErrInvalidArgument, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var actionResponse connectorRuntimeActionResponse
	if err := json.Unmarshal(payload, &actionResponse); err != nil {
		return nil, connectorActionTransportError(ctx, fmt.Errorf("%w: invalid connector action response: %v", apperrors.ErrInvalidArgument, err), needles)
	}
	if !actionResponse.OK {
		if len(needles) > 0 {
			return nil, fmt.Errorf("%w: credential connector action failed", apperrors.ErrInvalidArgument)
		}
		return nil, fmt.Errorf("%w: connector action failed: %s", apperrors.ErrInvalidArgument, connectorRuntimeErrorText(actionResponse.Error))
	}
	if len(needles) > 0 {
		actionResponse.Output = redactConnectorCredentialOutput(actionResponse.Output, needles)
	}
	return actionResponse.Output, nil
}

func (p *connectorRuntimeProvider) relatedIDs(actionName string) map[string]any {
	related := map[string]any{
		"actionName": strings.TrimSpace(actionName),
	}
	if p.pluginID != "" {
		related["pluginId"] = p.pluginID
	}
	if p.connectorID != "" {
		related["connectorId"] = p.connectorID
	}
	return related
}

func connectorRuntimeErrorText(value any) string {
	if value == nil {
		return "unknown connector error"
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(raw)
	}
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
