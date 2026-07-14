package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type KnowledgeModelRequest struct {
	PublicModel string
	RouteID     string
	Endpoint    string
	Payload     map[string]any
	RequestID   string
}

type KnowledgeModelResponse struct {
	Body         []byte
	PublicModel  string
	RouteID      string
	UpstreamID   string
	ProviderKind string
	Status       int
	RequestID    string
}

// InvokeKnowledgeModel provides a governed raw relay boundary for production
// retrieval adapters. It deliberately supports only embedding and rerank APIs.
func (s *Service) InvokeKnowledgeModel(
	ctx context.Context,
	principal domainidentity.Principal,
	input KnowledgeModelRequest,
) (KnowledgeModelResponse, error) {
	if s == nil || !s.relayConfig.Enabled {
		return KnowledgeModelResponse{}, fmt.Errorf("%w: AI Gateway LLM relay is disabled", apperrors.ErrNotFound)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayInvoke); err != nil {
		return KnowledgeModelResponse{}, err
	}
	endpoint := strings.ToLower(strings.TrimSpace(input.Endpoint))
	if endpoint != "embeddings" && endpoint != "rerank" {
		return KnowledgeModelResponse{}, fmt.Errorf("%w: knowledge model endpoint is not supported", apperrors.ErrInvalidArgument)
	}
	publicModel, routeID, err := s.resolveWorkbenchRelayModel(ctx, WorkbenchRelayRequest{
		PublicModel: input.PublicModel,
		RouteID:     input.RouteID,
	})
	if err != nil {
		return KnowledgeModelResponse{}, err
	}
	providerKind, selections, err := s.workbenchRelaySelections(ctx, principal, publicModel, routeID, endpoint)
	if err != nil {
		return KnowledgeModelResponse{}, err
	}
	payload := copyMap(input.Payload)
	payload["model"] = publicModel
	body, err := json.Marshal(payload)
	if err != nil {
		return KnowledgeModelResponse{}, fmt.Errorf("encode knowledge model request: %w", err)
	}
	req := LLMRelayHTTPRequest{
		ProviderKind: providerKind,
		Endpoint:     endpoint,
		Method:       http.MethodPost,
		Headers:      http.Header{"Content-Type": []string{"application/json"}},
		Body:         body,
		RequestID:    strings.TrimSpace(input.RequestID),
		UserAgent:    "opensoha-knowledge",
	}
	accessCtx := workbenchRelayAccessContext(principal, WorkbenchRelayRequest{
		Mode:     "knowledge-retrieval",
		Metadata: map[string]any{"requestId": req.RequestID},
	})
	writer := &workbenchRelayResponseWriter{header: http.Header{}}
	if err := s.proxyRelayRequestWithFallback(ctx, principal, accessCtx, req, selections, publicModel, false, writer); err != nil {
		return KnowledgeModelResponse{}, err
	}
	status := writer.status
	if status == 0 {
		status = http.StatusOK
	}
	if status >= http.StatusBadRequest {
		return KnowledgeModelResponse{}, fmt.Errorf("%w: knowledge model relay returned status %d", apperrors.ErrClusterUnready, status)
	}
	return KnowledgeModelResponse{
		Body:         append([]byte{}, writer.body.Bytes()...),
		PublicModel:  publicModel,
		RouteID:      selections[0].route.ID,
		UpstreamID:   selections[0].upstream.ID,
		ProviderKind: selections[0].upstreamProviderKind(),
		Status:       status,
		RequestID:    req.RequestID,
	}, nil
}
