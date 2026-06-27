package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	workbenchRelaySource    = "ai-workbench"
	workbenchRelayTokenKind = "internal_workbench"
)

type WorkbenchRelayMessage struct {
	Role    string
	Content string
}

type WorkbenchRelayRequest struct {
	PublicModel string
	RouteID     string
	Endpoint    string
	Messages    []WorkbenchRelayMessage
	SessionID   string
	AgentRunID  string
	AnalysisID  string
	Mode        string
	Metadata    map[string]any
}

type WorkbenchRelayResponse struct {
	Content      string
	PublicModel  string
	RouteID      string
	UpstreamID   string
	ProviderKind string
	Endpoint     string
	Status       int
	RequestID    string
}

func (s *Service) InvokeWorkbenchModel(ctx context.Context, principal domainidentity.Principal, input WorkbenchRelayRequest) (WorkbenchRelayResponse, error) {
	if s == nil {
		return WorkbenchRelayResponse{}, fmt.Errorf("%w: AI Gateway service is not configured", apperrors.ErrInvalidArgument)
	}
	if !s.relayConfig.Enabled {
		return WorkbenchRelayResponse{}, fmt.Errorf("%w: AI Gateway LLM relay is disabled", apperrors.ErrNotFound)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse); err != nil {
		return WorkbenchRelayResponse{}, err
	}
	publicModel, routeID, err := s.resolveWorkbenchRelayModel(ctx, input)
	if err != nil {
		return WorkbenchRelayResponse{}, err
	}
	endpoint := normalizeWorkbenchRelayEndpoint(input.Endpoint)
	providerKind, selections, err := s.workbenchRelaySelections(ctx, principal, publicModel, routeID, endpoint)
	if err != nil {
		return WorkbenchRelayResponse{}, err
	}
	body, err := workbenchRelayRequestBody(endpoint, publicModel, input.Messages)
	if err != nil {
		return WorkbenchRelayResponse{}, err
	}
	accessCtx := workbenchRelayAccessContext(principal, input)
	req := LLMRelayHTTPRequest{
		ProviderKind: providerKind,
		Endpoint:     endpoint,
		Method:       http.MethodPost,
		Headers:      http.Header{"Content-Type": []string{"application/json"}},
		Body:         body,
		RequestID:    workbenchRelayRequestID(accessCtx.Metadata),
		UserAgent:    "opensoha-ai-workbench",
	}
	writer := &workbenchRelayResponseWriter{header: http.Header{}}
	if err := s.proxyRelayRequestWithFallback(ctx, principal, accessCtx, req, selections, publicModel, false, writer); err != nil {
		return WorkbenchRelayResponse{}, err
	}
	status := writer.status
	if status == 0 {
		status = http.StatusOK
	}
	if status >= http.StatusBadRequest {
		return WorkbenchRelayResponse{}, fmt.Errorf("%w: workbench relay returned status %d", apperrors.ErrClusterUnready, status)
	}
	content, err := workbenchRelayResponseText(endpoint, writer.body.Bytes())
	if err != nil {
		return WorkbenchRelayResponse{}, err
	}
	return WorkbenchRelayResponse{
		Content:      content,
		PublicModel:  publicModel,
		RouteID:      selections[0].route.ID,
		UpstreamID:   selections[0].upstream.ID,
		ProviderKind: selections[0].upstreamProviderKind(),
		Endpoint:     endpoint,
		Status:       status,
		RequestID:    req.RequestID,
	}, nil
}

func (s *Service) resolveWorkbenchRelayModel(ctx context.Context, input WorkbenchRelayRequest) (string, string, error) {
	publicModel := strings.TrimSpace(input.PublicModel)
	routeID := strings.TrimSpace(input.RouteID)
	if publicModel != "" {
		return publicModel, routeID, nil
	}
	if routeID == "" {
		return "", "", fmt.Errorf("%w: workbench relay public model or route ID is required", apperrors.ErrInvalidArgument)
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return "", "", fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	route, err := repo.GetLLMModelRoute(ctx, routeID)
	if err != nil {
		return "", "", err
	}
	publicModel = strings.TrimSpace(route.PublicModel)
	if publicModel == "" {
		return "", "", fmt.Errorf("%w: workbench relay route public model is required", apperrors.ErrInvalidArgument)
	}
	return publicModel, routeID, nil
}

func (s *Service) workbenchRelaySelections(ctx context.Context, principal domainidentity.Principal, publicModel, routeID, endpoint string) (string, []relaySelection, error) {
	providerKind := workbenchRelayProviderForEndpoint(endpoint)
	if routeID != "" {
		return s.workbenchRelaySelectionsForRoute(ctx, principal, providerKind, publicModel, routeID)
	}
	selections, err := s.selectRelayUpstreamCandidatesForPrincipal(ctx, principal, providerKind, publicModel)
	if err != nil {
		return "", nil, err
	}
	if len(selections) == 0 {
		return "", nil, fmt.Errorf("%w: no active relay route for model %s", apperrors.ErrNotFound, publicModel)
	}
	return providerKind, selections, nil
}

func (s *Service) workbenchRelaySelectionsForRoute(ctx context.Context, principal domainidentity.Principal, fallbackProviderKind, publicModel, routeID string) (string, []relaySelection, error) {
	route, err := s.workbenchRelayRoute(ctx, routeID)
	if err != nil {
		return "", nil, err
	}
	providerKind := workbenchRelayProviderForRoute(route, fallbackProviderKind)
	selections, err := s.selectRelayUpstreamCandidatesForPrincipal(ctx, principal, providerKind, publicModel)
	if err != nil {
		return "", nil, err
	}
	selections = filterRelaySelectionsByRoute(selections, routeID)
	if len(selections) == 0 {
		return "", nil, fmt.Errorf("%w: route %s is not available for model %s", apperrors.ErrNotFound, routeID, publicModel)
	}
	return providerKind, selections, nil
}

func (s *Service) workbenchRelayRoute(ctx context.Context, routeID string) (domainaigateway.LLMModelRoute, error) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMModelRoute{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo.GetLLMModelRoute(ctx, strings.TrimSpace(routeID))
}

func workbenchRelayProviderForRoute(route domainaigateway.LLMModelRoute, fallbackProviderKind string) string {
	routeProvider := normalizeRelayProviderKind(route.ProviderKind)
	if routeProvider != "" &&
		!relayTransformPlanForRoute(route, fallbackProviderKind).enabled &&
		relayProviderUsesOpenAIWireProtocol(routeProvider) &&
		relayProviderUsesOpenAIWireProtocol(fallbackProviderKind) {
		return routeProvider
	}
	return fallbackProviderKind
}

func workbenchRelayProviderForEndpoint(endpoint string) string {
	switch strings.TrimSpace(endpoint) {
	case "messages":
		return "anthropic"
	case "chat/completions", "responses":
		return "openai"
	default:
		return "openai"
	}
}

func filterRelaySelectionsByRoute(selections []relaySelection, routeID string) []relaySelection {
	routeID = strings.TrimSpace(routeID)
	if routeID == "" {
		return selections
	}
	out := make([]relaySelection, 0, len(selections))
	for _, selection := range selections {
		if selection.route.ID == routeID {
			out = append(out, selection)
		}
	}
	return out
}

func normalizeWorkbenchRelayEndpoint(endpoint string) string {
	endpoint = strings.Trim(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return "chat/completions"
	}
	return endpoint
}

func workbenchRelayRequestID(metadata map[string]any) string {
	if requestID, ok := metadata["requestId"].(string); ok && strings.TrimSpace(requestID) != "" {
		return strings.TrimSpace(requestID)
	}
	return uuid.NewString()
}

func workbenchRelayAccessContext(principal domainidentity.Principal, input WorkbenchRelayRequest) domainidentity.AccessContext {
	metadata := copyMap(input.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["source"] = workbenchRelaySource
	if sessionID := strings.TrimSpace(input.SessionID); sessionID != "" {
		metadata["sessionId"] = sessionID
	}
	if agentRunID := strings.TrimSpace(input.AgentRunID); agentRunID != "" {
		metadata["agentRunId"] = agentRunID
	}
	if analysisID := strings.TrimSpace(input.AnalysisID); analysisID != "" {
		metadata["analysisRunId"] = analysisID
	}
	if mode := strings.TrimSpace(input.Mode); mode != "" {
		metadata["workbenchMode"] = mode
	}
	metadata["internal"] = true
	return domainidentity.AccessContext{
		TokenID:     "internal:ai-workbench",
		TokenKind:   workbenchRelayTokenKind,
		TokenPrefix: "internal",
		SessionID:   strings.TrimSpace(input.SessionID),
		SubjectType: "user",
		SubjectID:   principal.UserID,
		Scopes:      []string{"relay", "workbench"},
		Metadata:    metadata,
	}
}

func workbenchRelayRequestBody(endpoint, publicModel string, messages []WorkbenchRelayMessage) ([]byte, error) {
	switch endpoint {
	case "chat/completions":
		return json.Marshal(map[string]any{
			"model":       publicModel,
			"messages":    workbenchOpenAIMessages(messages),
			"temperature": 0.2,
			"stream":      false,
		})
	case "responses":
		return json.Marshal(map[string]any{
			"model":       publicModel,
			"input":       workbenchResponsesInput(messages),
			"temperature": 0.2,
			"stream":      false,
		})
	case "messages":
		system, chatMessages := workbenchAnthropicMessages(messages)
		payload := map[string]any{
			"model":       publicModel,
			"messages":    chatMessages,
			"max_tokens":  1024,
			"temperature": 0.2,
			"stream":      false,
		}
		if system != "" {
			payload["system"] = system
		}
		return json.Marshal(payload)
	default:
		return nil, fmt.Errorf("%w: workbench relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func workbenchOpenAIMessages(messages []WorkbenchRelayMessage) []map[string]string {
	out := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		role := normalizeWorkbenchMessageRole(message.Role)
		if role == "" {
			continue
		}
		out = append(out, map[string]string{"role": role, "content": strings.TrimSpace(message.Content)})
	}
	if len(out) == 0 {
		out = append(out, map[string]string{"role": "user", "content": ""})
	}
	return out
}

func workbenchResponsesInput(messages []WorkbenchRelayMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := normalizeWorkbenchMessageRole(message.Role)
		if role == "" {
			continue
		}
		out = append(out, map[string]any{
			"role": role,
			"content": []map[string]string{
				{"type": "input_text", "text": strings.TrimSpace(message.Content)},
			},
		})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "input_text", "text": ""}},
		})
	}
	return out
}

func workbenchAnthropicMessages(messages []WorkbenchRelayMessage) (string, []map[string]string) {
	var system []string
	out := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		role := normalizeWorkbenchMessageRole(message.Role)
		content := strings.TrimSpace(message.Content)
		switch role {
		case "system":
			if content != "" {
				system = append(system, content)
			}
		case "user", "assistant":
			out = append(out, map[string]string{"role": role, "content": content})
		}
	}
	if len(out) == 0 {
		out = append(out, map[string]string{"role": "user", "content": ""})
	}
	return strings.Join(system, "\n\n"), out
}

func normalizeWorkbenchMessageRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "assistant", "user":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return ""
	}
}

type workbenchRelayResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *workbenchRelayResponseWriter) Header() http.Header {
	return w.header
}

func (w *workbenchRelayResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *workbenchRelayResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func workbenchRelayResponseText(endpoint string, body []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%w: invalid workbench relay response JSON", apperrors.ErrClusterUnready)
	}
	var text string
	switch endpoint {
	case "chat/completions":
		text = workbenchOpenAIResponseText(payload)
	case "responses":
		text = workbenchResponsesResponseText(payload)
	case "messages":
		if value, ok := relayTextFromContent(payload["content"]); ok {
			text = value
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%w: workbench relay response content is empty", apperrors.ErrClusterUnready)
	}
	return text, nil
}

func workbenchOpenAIResponseText(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	if message, ok := choice["message"].(map[string]any); ok {
		if text, ok := relayTextFromContent(message["content"]); ok {
			return text
		}
	}
	if text, ok := choice["text"].(string); ok {
		return text
	}
	return ""
}

func workbenchResponsesResponseText(payload map[string]any) string {
	if text, ok := payload["output_text"].(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	output, _ := payload["output"].([]any)
	var builder strings.Builder
	for _, raw := range output {
		item, _ := raw.(map[string]any)
		content, _ := item["content"].([]any)
		for _, rawBlock := range content {
			block, _ := rawBlock.(map[string]any)
			if text, ok := block["text"].(string); ok {
				builder.WriteString(text)
			}
		}
	}
	return builder.String()
}
