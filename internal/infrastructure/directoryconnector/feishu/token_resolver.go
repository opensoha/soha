package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type LoginProviderResolver interface {
	ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error)
}

type tenantTokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

// NewTenantTokenResolver exchanges the existing login provider's app
// credentials for a tenant token. Tokens are retained in memory only.
func NewTenantTokenResolver(resolver LoginProviderResolver, client *http.Client, baseURL string) TokenResolver {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	var mu sync.Mutex
	cache := map[string]tenantTokenCacheEntry{}
	return func(ctx context.Context, connection domain.Connection) (string, error) {
		if resolver == nil {
			return "", errors.New("login provider resolver is required")
		}
		providerID := strings.TrimSpace(connection.LoginProviderID)
		if providerID == "" {
			return "", errors.New("Feishu directory connection requires loginProviderId")
		}
		mu.Lock()
		cached := cache[providerID]
		if cached.token != "" && time.Until(cached.expiresAt) > time.Minute {
			mu.Unlock()
			return cached.token, nil
		}
		mu.Unlock()
		provider, err := resolver.ResolveLoginProvider(ctx, providerID)
		if err != nil {
			return "", fmt.Errorf("resolve login provider: %w", err)
		}
		if !provider.Enabled || provider.ClientID == "" || provider.ClientSecret == "" {
			return "", errors.New("Feishu login provider must be enabled and contain app credentials")
		}
		body, _ := json.Marshal(map[string]string{"app_id": provider.ClientID, "app_secret": provider.ClientSecret})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("build Feishu tenant token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("request Feishu tenant token: %w", err)
		}
		defer resp.Body.Close()
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return "", fmt.Errorf("read Feishu tenant token response: %w", err)
		}
		var envelope struct {
			Code              int    `json:"code"`
			Msg               string `json:"msg"`
			TenantAccessToken string `json:"tenant_access_token"`
			Expire            int    `json:"expire"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return "", errors.New("Feishu tenant token response is invalid JSON")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != 0 || envelope.TenantAccessToken == "" {
			return "", fmt.Errorf("Feishu tenant token exchange failed: status=%d code=%d message=%q", resp.StatusCode, envelope.Code, envelope.Msg)
		}
		expiresIn := time.Duration(envelope.Expire) * time.Second
		if expiresIn <= 0 {
			expiresIn = time.Hour
		}
		mu.Lock()
		cache[providerID] = tenantTokenCacheEntry{token: envelope.TenantAccessToken, expiresAt: time.Now().Add(expiresIn)}
		mu.Unlock()
		return envelope.TenantAccessToken, nil
	}
}
