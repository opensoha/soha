package aigateway

import (
	"context"
	"encoding/json"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const knowledgeSearchToolName = "knowledge.search"

type KnowledgeSearchService interface {
	Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error)
}

type knowledgeCapabilityProvider struct {
	search KnowledgeSearchService
}

func NewKnowledgeCapabilityProvider(search KnowledgeSearchService) CapabilityProvider {
	return &knowledgeCapabilityProvider{search: search}
}

func (p *knowledgeCapabilityProvider) Tools() []domainaigateway.ToolCapability {
	return []domainaigateway.ToolCapability{{
		Name: "knowledge.search", Title: "Search Knowledge", Description: "Search authorized knowledge bases and return grounded chunks with citations.",
		Domain: "knowledge", Action: "search", RiskLevel: domainaigateway.RiskLevelRead,
		PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIKnowledgeView},
		RequiredScopes: []string{"aiClient", "skill"}, MCPAdapterID: "knowledge.v1", MCPToolName: knowledgeSearchToolName,
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"knowledgeBaseIds", "query"},
			"properties": map[string]any{
				"knowledgeBaseIds": map[string]any{"type": "array", "minItems": 1, "maxItems": 32, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 160}},
				"query":            map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
				"topK":             map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
				"filters": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{
						"sourceIds":   map[string]any{"type": "array", "maxItems": 128, "items": map[string]any{"type": "string", "maxLength": 160}},
						"documentIds": map[string]any{"type": "array", "maxItems": 128, "items": map[string]any{"type": "string", "maxLength": 160}},
					},
				},
			},
		},
	}}
}

func (*knowledgeCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*knowledgeCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*knowledgeCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }

func (p *knowledgeCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if tool.Name != knowledgeSearchToolName || p == nil || p.search == nil {
		return nil, nil, fmt.Errorf("%w: knowledge search provider is unavailable", apperrors.ErrUnsupportedOperation)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: encode knowledge search input", apperrors.ErrInvalidArgument)
	}
	var request domainknowledge.SearchRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, nil, fmt.Errorf("%w: decode knowledge search input", apperrors.ErrInvalidArgument)
	}
	result, err := p.search.Search(ctx, principal, request)
	if err != nil {
		return nil, nil, err
	}
	return result, map[string]any{
		"knowledgeBaseIds": append([]string(nil), request.KnowledgeBaseIDs...),
		"traceId":          result.TraceID,
	}, nil
}
