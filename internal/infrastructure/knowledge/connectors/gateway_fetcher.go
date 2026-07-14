package connectors

import (
	"context"
	"encoding/json"
	"fmt"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const knowledgeConnectorWorkerClient = "knowledge-ingestion-worker"

type GatewayToolInvoker interface {
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
}

type GatewayFetcher struct{ invoker GatewayToolInvoker }

func NewGatewayFetcher(invoker GatewayToolInvoker) (*GatewayFetcher, error) {
	if invoker == nil {
		return nil, fmt.Errorf("knowledge connector Gateway invoker is required")
	}
	return &GatewayFetcher{invoker: invoker}, nil
}

func (f *GatewayFetcher) Fetch(ctx context.Context, request FetchRequest) (FetchResult, error) {
	toolName := map[domainknowledge.SourceKind]string{
		domainknowledge.SourceKindHTTP:   "knowledge.connector.http.fetch",
		domainknowledge.SourceKindGit:    "knowledge.connector.git.fetch",
		domainknowledge.SourceKindObject: "knowledge.connector.object.fetch",
	}[request.Kind]
	if toolName == "" {
		return FetchResult{}, fmt.Errorf("%w: unsupported connector kind", domainknowledge.ErrInvalidInput)
	}
	result, err := f.invoker.InvokeTool(ctx, request.Principal, domainaigateway.ToolInvocationRequest{
		ToolName: toolName, AIClientID: knowledgeConnectorWorkerClient,
		AIClientName: "Knowledge Ingestion Worker", SkillID: "knowledge-connector-operator",
		Input: map[string]any{
			"schemaVersion": "opensoha.dev/knowledge-connector-fetch/v1",
			"secretRef":     request.SecretRef, "config": request.Config, "cursor": request.Cursor,
			"networkPolicy": map[string]any{"allowedHosts": request.AllowedHosts, "denyPrivateNetworks": request.DenyPrivateNetworks, "maxRedirects": request.MaxRedirects},
			"limits":        map[string]any{"maxItems": request.MaxItems, "maxBytes": request.MaxBytes},
		},
	})
	if err != nil {
		return FetchResult{}, fmt.Errorf("invoke connector plugin %s: %w", request.Kind, err)
	}
	payload, err := json.Marshal(result.Output)
	if err != nil {
		return FetchResult{}, fmt.Errorf("encode connector plugin output: %w", err)
	}
	var output FetchResult
	decoder := json.Unmarshal(payload, &output)
	if decoder != nil {
		return FetchResult{}, fmt.Errorf("decode connector plugin output: %w", decoder)
	}
	return output, nil
}
