package aigateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"slices"
	"sort"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type WorkbenchImageRequest struct{ Prompt, Image, PublicModel, SessionID string }

// Image work uses the same owner-scoped access, route policy and audited relay as chat.
func (s *Service) InvokeWorkbenchImage(ctx context.Context, principal domainidentity.Principal, input WorkbenchImageRequest) ([]byte, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return nil, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse); err != nil {
		return nil, err
	}
	if !s.relayConfig.Enabled {
		return nil, apperrors.ErrNotFound
	}
	if strings.TrimSpace(input.Prompt) == "" || len([]rune(input.Prompt)) > 2000 || len(input.Image) > 700000 {
		return nil, apperrors.ErrInvalidArgument
	}
	endpoint := "images/generations"
	if input.Image != "" {
		endpoint = "images/edits"
	}
	model, routeID, err := s.workbenchImageModel(ctx, principal, input.PublicModel, endpoint)
	if err != nil {
		return nil, err
	}
	provider, selections, err := s.workbenchRelaySelections(ctx, principal, model, routeID, endpoint)
	if err != nil {
		return nil, err
	}
	body, contentType, err := workbenchImageBody(model, selections[0].route.UpstreamModel, input)
	if err != nil {
		return nil, err
	}
	access := workbenchRelayAccessContext(principal, WorkbenchRelayRequest{SessionID: input.SessionID, Mode: "assistant-avatar"})
	req := LLMRelayHTTPRequest{ProviderKind: provider, Endpoint: endpoint, Method: http.MethodPost, Headers: http.Header{"Content-Type": {contentType}}, Body: body, RequestID: workbenchRelayRequestID(access.Metadata), UserAgent: "opensoha-ai-workbench"}
	writer := &workbenchImageWriter{workbenchRelayResponseWriter: workbenchRelayResponseWriter{header: http.Header{}}}
	if err := s.proxyRelayRequestWithFallback(ctx, principal, access, req, selections, model, false, writer); err != nil {
		return nil, err
	}
	if writer.status >= 400 {
		return nil, fmt.Errorf("%w: image relay failed", apperrors.ErrClusterUnready)
	}
	var result struct {
		Data []struct {
			Base64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(writer.body.Bytes(), &result); err != nil || len(result.Data) == 0 || result.Data[0].Base64 == "" {
		return nil, fmt.Errorf("%w: image relay must return an inline image", apperrors.ErrUnsupportedOperation)
	}
	data, err := base64.StdEncoding.Strict().DecodeString(result.Data[0].Base64)
	if err != nil || len(data) > 5<<20 {
		return nil, apperrors.ErrInvalidArgument
	}
	return data, nil
}

func (s *Service) workbenchImageModel(ctx context.Context, principal domainidentity.Principal, requested, endpoint string) (string, string, error) {
	requested = strings.TrimSpace(requested)
	repo := s.llmRelayRepository()
	if repo == nil {
		return "", "", apperrors.ErrNotFound
	}
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{})
	if err != nil {
		return "", "", err
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Priority != routes[j].Priority {
			return routes[i].Priority < routes[j].Priority
		}
		return routes[i].ID < routes[j].ID
	})
	for _, route := range routes {
		if !route.Enabled || (requested != "" && route.PublicModel != requested) || !slices.Contains(gatewayConditionStringList(route.Metadata, "imageEndpoints"), endpoint) {
			continue
		}
		if _, _, err := s.workbenchRelaySelections(ctx, principal, route.PublicModel, route.ID, endpoint); err == nil {
			return route.PublicModel, route.ID, nil
		}
	}
	return "", "", apperrors.ErrNotFound
}

func workbenchImageBody(model, upstreamModel string, input WorkbenchImageRequest) ([]byte, string, error) {
	prompt := input.Prompt + "。生成适合圆形裁切的助手头像，主体居中，无文字。"
	fields := map[string]string{"model": model, "prompt": prompt, "n": "1", "size": "1024x1024"}
	// GPT Image always returns base64 and rejects the legacy response_format parameter.
	if !strings.HasPrefix(upstreamModel, "gpt-image-") && upstreamModel != "chatgpt-image-latest" {
		fields["response_format"] = "b64_json"
	}
	if input.Image == "" {
		payload := map[string]any{"model": model, "prompt": prompt, "n": 1, "size": "1024x1024"}
		if fields["response_format"] != "" {
			payload["response_format"] = fields["response_format"]
		}
		data, err := json.Marshal(payload)
		return data, "application/json", err
	}
	_, encoded, ok := strings.Cut(input.Image, ";base64,")
	if !ok {
		return nil, "", apperrors.ErrInvalidArgument
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, "", apperrors.ErrInvalidArgument
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	filename := "avatar.png"
	if http.DetectContentType(data) == "image/jpeg" {
		filename = "avatar.jpg"
	}
	file, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err = file.Write(data); err != nil {
		return nil, "", err
	}
	if err = writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

type workbenchImageWriter struct{ workbenchRelayResponseWriter }

func (w *workbenchImageWriter) Write(data []byte) (int, error) {
	if w.body.Len()+len(data) > 7<<20 {
		return 0, apperrors.ErrInvalidArgument
	}
	return w.workbenchRelayResponseWriter.Write(data)
}
