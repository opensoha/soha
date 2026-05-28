package sohacli

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
)

const userAgent = "soha-cli/0.1"

type APIClient struct {
	ServerURL string
	Token     string
	Client    *http.Client
}

type loginResponse struct {
	Data struct {
		User struct {
			UserID   string   `json:"userId"`
			UserName string   `json:"userName"`
			Roles    []string `json:"roles"`
		} `json:"user"`
		Tokens struct {
			AccessToken  string    `json:"accessToken"`
			RefreshToken string    `json:"refreshToken"`
			ExpiresAt    time.Time `json:"expiresAt"`
		} `json:"tokens"`
	} `json:"data"`
}

type manifestResponse struct {
	Data Manifest `json:"data"`
}

type invokeResponse struct {
	Data ToolInvocationResult `json:"data"`
}

type Manifest struct {
	Name           string               `json:"name"`
	Version        string               `json:"version"`
	PermissionKeys []string             `json:"permissionKeys"`
	Tools          []ToolCapability     `json:"tools"`
	Resources      []ResourceCapability `json:"resources,omitempty"`
	Prompts        []PromptCapability   `json:"prompts,omitempty"`
	Skills         []SkillCapability    `json:"skills,omitempty"`
	Summary        map[string]any       `json:"summary,omitempty"`
}

type ToolCapability struct {
	Name             string         `json:"name"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Domain           string         `json:"domain"`
	Action           string         `json:"action"`
	RiskLevel        string         `json:"riskLevel"`
	PermissionKeys   []string       `json:"permissionKeys"`
	RequiredScopes   []string       `json:"requiredScopes,omitempty"`
	MCPAdapterID     string         `json:"mcpAdapterId,omitempty"`
	MCPToolName      string         `json:"mcpToolName,omitempty"`
	RequiresApproval bool           `json:"requiresApproval"`
	InputSchema      map[string]any `json:"inputSchema,omitempty"`
	OutputSchema     map[string]any `json:"outputSchema,omitempty"`
}

type ResourceCapability struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permissionKeys,omitempty"`
}

type PromptCapability struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permissionKeys,omitempty"`
}

type SkillCapability struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	CapabilityRefs []string `json:"capabilityRefs,omitempty"`
	PermissionKeys []string `json:"permissionKeys,omitempty"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
}

type ToolInvocationResult struct {
	ToolName         string         `json:"toolName"`
	RiskLevel        string         `json:"riskLevel"`
	RequiresApproval bool           `json:"requiresApproval"`
	Result           string         `json:"result"`
	Output           any            `json:"output,omitempty"`
	RelatedIDs       map[string]any `json:"relatedIds,omitempty"`
	Audit            map[string]any `json:"audit,omitempty"`
}

func (c APIClient) Login(ctx context.Context, login, password string) (loginResponse, error) {
	var out loginResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/login", "", nil, map[string]string{
		"login":    login,
		"password": password,
	}, &out)
	return out, err
}

func (c APIClient) Capabilities(ctx context.Context, headers map[string]string) (Manifest, error) {
	var out manifestResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/ai-gateway/capabilities", c.Token, headers, nil, &out); err != nil {
		return Manifest{}, err
	}
	return out.Data, nil
}

func (c APIClient) InvokeTool(ctx context.Context, toolName string, input map[string]any, headers map[string]string) (ToolInvocationResult, error) {
	var out invokeResponse
	path := "/api/v1/ai-gateway/tools/" + url.PathEscape(toolName) + "/invoke"
	payload := map[string]any{"input": emptyInput(input)}
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, payload, &out); err != nil {
		return ToolInvocationResult{}, err
	}
	return out.Data, nil
}

func (c APIClient) doJSON(ctx context.Context, method, path, token string, headers map[string]string, body any, out any) error {
	base := strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	if base == "" {
		return fmt.Errorf("server URL is required")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		value = strings.TrimSpace(value)
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s failed: %s: %s", method, path, resp.Status, responseErrorMessage(raw))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func responseErrorMessage(raw []byte) string {
	var wrapped struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Error.Message != "" {
		if wrapped.Error.Code != "" {
			return wrapped.Error.Code + ": " + wrapped.Error.Message
		}
		return wrapped.Error.Message
	}
	return strings.TrimSpace(string(raw))
}

func emptyInput(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}
