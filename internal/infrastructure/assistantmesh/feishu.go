package assistantmesh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	domaindirectory "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	feishuActionPath    = "/actions/feishu.message.send_text"
	feishuTimeout       = 10 * time.Second
	feishuResponseLimit = 1 << 20
)

var feishuRequestIDPattern = regexp.MustCompile(`^heya-[a-f0-9]{32}$`)

type FeishuDirectoryRepo interface {
	GetConnection(context.Context, string) (domaindirectory.Connection, domaindirectory.Policy, error)
	ResolveActiveFeishuOpenID(context.Context, string, string, string) (string, error)
}

type FeishuLoginProviders interface {
	ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error)
}

// FeishuDirectory gates every resolution on the current connection and login
// provider settings so a later disable or app change invalidates old bindings.
type FeishuDirectory struct {
	repo         FeishuDirectoryRepo
	providers    FeishuLoginProviders
	connectionID string
	appID        string
}

func NewFeishuDirectory(repo FeishuDirectoryRepo, providers FeishuLoginProviders, connectionID, appID string) *FeishuDirectory {
	return &FeishuDirectory{repo: repo, providers: providers, connectionID: strings.TrimSpace(connectionID), appID: strings.TrimSpace(appID)}
}

func (d *FeishuDirectory) ResolveFeishuUser(ctx context.Context, openID string) (string, error) {
	if d == nil || d.repo == nil || d.providers == nil || d.connectionID == "" || d.appID == "" {
		return "", fmt.Errorf("%w: Feishu directory is not configured", apperrors.ErrNotFound)
	}
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return "", fmt.Errorf("%w: Feishu open ID is required", apperrors.ErrInvalidArgument)
	}
	connection, policy, err := d.repo.GetConnection(ctx, d.connectionID)
	if err != nil {
		return "", err
	}
	if !connection.Enabled || connection.ProviderType != domaindirectory.ProviderFeishu || !policy.SyncPeople || strings.TrimSpace(connection.LoginProviderID) == "" {
		return "", fmt.Errorf("%w: Feishu directory binding", apperrors.ErrNotFound)
	}
	providerID := strings.TrimSpace(connection.LoginProviderID)
	provider, err := d.providers.ResolveLoginProvider(ctx, providerID)
	if err != nil {
		return "", err
	}
	if !provider.Enabled || provider.Type != domaindirectory.ProviderFeishu || strings.TrimSpace(provider.ClientID) != d.appID {
		return "", fmt.Errorf("%w: Feishu login provider binding", apperrors.ErrNotFound)
	}
	return d.repo.ResolveActiveFeishuOpenID(ctx, d.connectionID, providerID, openID)
}

type feishuActionInput struct {
	ReceiveIDType string `json:"receiveIdType"`
	ReceiveID     string `json:"receiveId"`
	Text          string `json:"text"`
}

type feishuActionEvent struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Source     string         `json:"source"`
	OccurredAt string         `json:"occurredAt"`
	Payload    map[string]any `json:"payload"`
}

type feishuActionEnvelope struct {
	Action    string            `json:"action"`
	Input     feishuActionInput `json:"input"`
	RequestID string            `json:"requestId"`
	Event     feishuActionEvent `json:"event"`
}

type feishuActionReply struct {
	OK    bool `json:"ok"`
	Error *struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

type FeishuSender struct {
	actionURL string
	token     string
	appID     string
	client    *http.Client
	now       func() time.Time
}

func NewFeishuSender(endpoint, token, appID string) (*FeishuSender, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	token = strings.TrimSpace(token)
	appID = strings.TrimSpace(appID)
	if endpoint == "" || token == "" || appID == "" {
		return nil, fmt.Errorf("%w: Feishu sender endpoint, token and app ID are required", apperrors.ErrInvalidArgument)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: Feishu sender endpoint is invalid", apperrors.ErrInvalidArgument)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && plainHTTPAllowed(parsed.Hostname())) {
		return nil, fmt.Errorf("%w: Feishu sender endpoint must use HTTPS", apperrors.ErrInvalidArgument)
	}
	return &FeishuSender{
		actionURL: endpoint + feishuActionPath,
		token:     token,
		appID:     appID,
		now:       time.Now,
		client: &http.Client{
			Timeout:       feishuTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// SendFeishuText makes exactly one transport attempt. A retryable result keeps
// the durable job pending so authorization is checked again before resend.
func (s *FeishuSender) SendFeishuText(ctx context.Context, idType, recipient, requestID, text string) (retryable bool, err error) {
	if s == nil || s.client == nil {
		return false, fmt.Errorf("%w: Feishu sender is not configured", apperrors.ErrInvalidArgument)
	}
	idType = strings.TrimSpace(idType)
	recipient = strings.TrimSpace(recipient)
	if !validFeishuRecipient(idType, recipient) {
		return false, fmt.Errorf("%w: Feishu receive ID type or value is invalid", apperrors.ErrInvalidArgument)
	}
	if !feishuRequestIDPattern.MatchString(requestID) {
		return false, fmt.Errorf("%w: Feishu request ID is invalid", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(text) == "" {
		return false, fmt.Errorf("%w: Feishu text is empty", apperrors.ErrInvalidArgument)
	}

	body, err := json.Marshal(feishuActionEnvelope{
		Action:    "feishu.message.send_text",
		Input:     feishuActionInput{ReceiveIDType: idType, ReceiveID: recipient, Text: safeFeishuText(text)},
		RequestID: requestID,
		Event: feishuActionEvent{
			ID: requestID, Type: "assistant_mesh_delivery", Source: "feishu",
			OccurredAt: s.now().UTC().Format(time.RFC3339), Payload: map[string]any{"appId": s.appID},
		},
	})
	if err != nil {
		return false, fmt.Errorf("%w: Feishu action envelope", apperrors.ErrInvalidArgument)
	}

	requestCtx, cancel := context.WithTimeout(ctx, feishuTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.actionURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build Feishu action request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)

	resp, err := s.client.Do(req)
	if err != nil {
		return true, fmt.Errorf("Feishu action transport failed: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, feishuResponseLimit+1))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return retryableStatus(resp.StatusCode), fmt.Errorf("read Feishu action response: %w", readErr)
	}
	if closeErr != nil {
		return retryableStatus(resp.StatusCode), fmt.Errorf("close Feishu action response: %w", closeErr)
	}

	retryable = retryableStatus(resp.StatusCode)
	var reply feishuActionReply
	if len(payload) <= feishuResponseLimit {
		if decodeErr := json.Unmarshal(payload, &reply); decodeErr == nil {
			if reply.Error != nil && reply.Error.Retryable {
				retryable = true
			}
			if resp.StatusCode == http.StatusOK && reply.OK {
				return false, nil
			}
		} else if resp.StatusCode == http.StatusOK {
			retryable = true
		}
	} else if resp.StatusCode == http.StatusOK {
		retryable = true
	}
	return retryable, fmt.Errorf("Feishu action failed with HTTP %d", resp.StatusCode)
}

func validFeishuRecipient(idType, recipient string) bool {
	switch idType {
	case "chat_id":
		return strings.HasPrefix(recipient, "oc_")
	case "open_id":
		return strings.HasPrefix(recipient, "ou_")
	default:
		return false
	}
}

// Feishu treats <at ...> as real mentions. Inserting a zero-width space keeps
// the user-visible text literal without pinging anyone.
func safeFeishuText(text string) string {
	return strings.ReplaceAll(text, "<", "<\u200b")
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func plainHTTPAllowed(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
