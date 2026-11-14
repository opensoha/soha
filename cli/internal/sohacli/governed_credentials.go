package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func governedBinding(metadata map[string]any) (*sohaapi.GovernedAITokenBinding, error) {
	value, present := metadata["governedAI"]
	if !present {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid governed AI binding")
	}
	var binding sohaapi.GovernedAITokenBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&binding) != nil || binding.Mode != "gateway_only" || binding.AIClientID == "" || binding.SkillID == "" {
		return nil, fmt.Errorf("invalid governed AI binding: gateway_only, aiClientId and skillId are required")
	}
	return &binding, nil
}

func applyGovernedCredentialInput(payload map[string]any, enabled bool, clientID, skillID string) error {
	if !enabled {
		if clientID != "" || skillID != "" {
			return usageError{message: "--ai-client-id and --skill-id require --governed-ai"}
		}
		return nil
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(skillID) == "" {
		return usageError{message: "--governed-ai requires --ai-client-id and --skill-id"}
	}
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, present := metadata["governedAI"]; present {
		return usageError{message: "specify governedAI using flags or --metadata-json, not both"}
	}
	metadata["governedAI"] = sohaapi.GovernedAITokenBinding{Mode: "gateway_only", AIClientID: strings.TrimSpace(clientID), SkillID: strings.TrimSpace(skillID)}
	payload["metadata"] = metadata
	return nil
}

func (c APIClient) governedCredentialPath(ctx context.Context, path string, binding *sohaapi.GovernedAITokenBinding) (string, error) {
	if binding == nil {
		return path, nil
	}
	options, err := c.CLIAuthOptions(ctx)
	if err != nil {
		return "", fmt.Errorf("governed_ai_unsupported: server capability check failed: %w", err)
	}
	if !options.SupportsGovernedAI {
		return "", fmt.Errorf("governed_ai_unsupported: this server cannot create or rotate governed credentials")
	}
	return strings.Replace(path, "/ai-gateway/", "/ai-gateway/governed-credentials/", 1), nil
}

func (c APIClient) verifyGovernedCredential(ctx context.Context, id, value, prefix string, metadata map[string]any, expected *sohaapi.GovernedAITokenBinding, serviceAccount bool) error {
	if expected == nil {
		return nil
	}
	actual, err := governedBinding(metadata)
	wantPrefix := "soha_gpat_"
	if serviceAccount {
		wantPrefix = "soha_gsat_"
	}
	if err == nil && actual != nil && *actual == *expected && strings.HasPrefix(value, wantPrefix) && strings.HasPrefix(prefix, wantPrefix) && strings.HasPrefix(value, prefix) && id != "" {
		return nil
	}
	// Never expose an unexpectedly unbound one-time secret. Revoke with the
	// independent management credential, then report only its public identifier.
	cleanup := "revocation_unconfirmed"
	if id != "" {
		var revokeErr error
		if serviceAccount {
			revokeErr = c.RevokeServiceAccountToken(ctx, id, nil)
		} else {
			revokeErr = c.RevokePersonalAccessToken(ctx, id, nil)
		}
		if revokeErr == nil {
			cleanup = "revoked"
		}
	}
	return fmt.Errorf("governed_credential_response_invalid: server did not preserve the required prefix and binding (%s; review Key %s)", cleanup, terminalLabel(id))
}

func (c APIClient) RotateGatewayCredential(ctx context.Context, kind, id string, input sohaapi.TokenRotationInput) (any, error) {
	var metadata map[string]any
	path := "/api/v1/ai-gateway/personal-access-tokens/" + url.PathEscape(id) + "/rotate"
	found := false
	serviceAccount := kind == "service_account_token"
	if serviceAccount {
		items, err := c.ListServiceAccountTokens(ctx, nil)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.ID == id {
				metadata, found = item.Metadata, true
				break
			}
		}
		path = "/api/v1/ai-gateway/service-account-tokens/" + url.PathEscape(id) + "/rotate"
	} else {
		items, err := c.ListPersonalAccessTokens(ctx, nil)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.ID == id {
				metadata, found = item.Metadata, true
				break
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("key was not found; no rotation was attempted")
	}
	binding, err := governedBinding(metadata)
	if err != nil {
		return nil, err
	}
	path, err = c.governedCredentialPath(ctx, path, binding)
	if err != nil {
		return nil, err
	}
	if serviceAccount {
		var out itemResponse[CreatedServiceAccountToken]
		if err = c.doJSON(ctx, http.MethodPost, path, c.Token, nil, input, &out); err != nil {
			return nil, err
		}
		err = c.verifyGovernedCredential(ctx, out.Data.Token.ID, out.Data.Value, out.Data.Token.TokenPrefix, out.Data.Token.Metadata, binding, true)
		return out.Data, err
	}
	var out itemResponse[CreatedPersonalAccessToken]
	if err = c.doJSON(ctx, http.MethodPost, path, c.Token, nil, input, &out); err != nil {
		return nil, err
	}
	err = c.verifyGovernedCredential(ctx, out.Data.Token.ID, out.Data.Value, out.Data.Token.TokenPrefix, out.Data.Token.Metadata, binding, false)
	return out.Data, err
}
