package identityprovider

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	outpostProtocolVersion       = "v1"
	outpostConfigurationLeaseTTL = 10 * time.Minute
)

type outpostRuntimeVersionRepository interface {
	ResolveOutpostRuntimeVersion(context.Context, string, string) (int64, error)
}

// Lease persistence is mandatory: process-local observations cannot establish a grant.
type outpostRuntimeLeaseRepository interface {
	RecordOutpostRuntimeLease(context.Context, string, string, string, int64, time.Time, time.Time) error
	ValidateOutpostRuntimeLease(context.Context, string, string, int64, *time.Time) error
}

func (s *Service) outpostLeaseRepository() (outpostRuntimeLeaseRepository, error) {
	repository, ok := s.repo.(outpostRuntimeLeaseRepository)
	if !ok {
		return nil, fmt.Errorf("%w: Outpost runtime lease repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	return repository, nil
}

func (s *Service) ClaimIdentityOutpostRuntime(ctx context.Context, token string, request sohaapi.IdentityOutpostClaimRequest) (*sohaapi.IdentityOutpostRuntimeConfig, error) {
	if err := s.requireOutpostRuntimeSigner(); err != nil {
		return nil, err
	}
	outpostID := strings.TrimSpace(request.AgentID)
	if outpostID == "" || len(request.SupportedProtocolVersion) > 32 || len(request.RuntimeVersion) > 128 {
		return nil, fmt.Errorf("%w: unsupported outpost identity or protocol version", apperrors.ErrInvalidArgument)
	}
	outpost, err := s.authenticateOutpost(ctx, outpostID, token)
	if err != nil {
		return nil, err
	}
	if outpost.Mode == domainprovider.OutpostModeEmbedded {
		return nil, fmt.Errorf("%w: embedded outposts do not claim remote runtime configuration", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(request.SupportedProtocolVersion) != outpostProtocolVersion {
		return nil, fmt.Errorf("%w: unsupported outpost protocol version", apperrors.ErrInvalidArgument)
	}
	if err := validateOutpostControlClaim(request); err != nil {
		return nil, err
	}
	firstClaim := outpost.ClaimedAgentID == ""
	now := time.Now().UTC()
	outpost.ClaimedAgentID = outpost.ID
	outpost.ProtocolVersion = strings.TrimSpace(request.SupportedProtocolVersion)
	outpost.RuntimeVersion = strings.TrimSpace(request.RuntimeVersion)
	outpost.Version = outpost.RuntimeVersion
	outpost.LastSeenAt, outpost.UpdatedAt, outpost.Metadata = &now, now, nil
	if _, err := s.repo.RecordOutpostClaim(ctx, outpost); err != nil {
		return nil, err
	}
	providers, err := s.outpostProxyProviders(ctx, outpost.ID)
	if err != nil {
		return nil, err
	}
	config, err := s.outpostRuntimeConfig(ctx, outpost, providers)
	if err != nil {
		return nil, err
	}
	if err := s.attachOutpostRuntimeControl(ctx, &config, request, outpost.TokenHash); err != nil {
		return nil, err
	}
	if firstClaim {
		s.recordAudit(ctx, domainidentity.Principal{}, "outpost.claim", "success", domainprovider.Provider{ID: outpost.ID, Type: "outpost"}, domainprovider.OIDCClient{}, map[string]any{"mode": outpost.Mode, "protocolVersion": outpost.ProtocolVersion})
	}
	return &config, nil
}

func (s *Service) HeartbeatIdentityOutpostRuntime(ctx context.Context, outpostID, token string, request sohaapi.IdentityOutpostHeartbeatRequest) (sohaapi.IdentityOutpostHeartbeat, error) {
	if err := s.requireOutpostRuntimeSigner(); err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	if strings.TrimSpace(request.AgentID) != strings.TrimSpace(outpostID) {
		return sohaapi.IdentityOutpostHeartbeat{}, fmt.Errorf("%w: outpost agent ID mismatch", apperrors.ErrAccessDenied)
	}
	outpost, err := s.authenticateOutpost(ctx, outpostID, token)
	if err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	if request.ConfigurationVersion < 0 || len(request.RuntimeVersion) > 128 || len(request.ErrorCode) > 128 {
		return sohaapi.IdentityOutpostHeartbeat{}, fmt.Errorf("%w: invalid applied configuration version", apperrors.ErrInvalidArgument)
	}
	if err := validateOutpostControlHeartbeat(request); err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	providers, err := s.outpostProxyProviders(ctx, outpostID)
	if err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	desired, err := s.resolveOutpostConfigurationVersion(ctx, outpostID, providers)
	if err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	if err := validateOutpostHeartbeatReport(request, desired, time.Now().UTC()); err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	leaseRepository, err := s.outpostLeaseRepository()
	if err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	if request.ConfigurationVersion > 0 {
		if err := leaseRepository.ValidateOutpostRuntimeLease(ctx, outpostID, outpost.TokenHash, request.ConfigurationVersion, request.ConfigurationExpiresAt); err != nil {
			return sohaapi.IdentityOutpostHeartbeat{}, err
		}
	}
	outpost.Status = domainprovider.OutpostStatusDegraded
	switch request.Status {
	case sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy:
		outpost.Status = domainprovider.OutpostStatusOnline
	case sohaapi.IdentityOutpostHeartbeatRequestStatusUnavailable:
		outpost.Status = domainprovider.OutpostStatusOffline
	case sohaapi.IdentityOutpostHeartbeatRequestStatusDegraded:
	default:
		return sohaapi.IdentityOutpostHeartbeat{}, fmt.Errorf("%w: invalid outpost heartbeat status", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	outpost.LastSeenAt, outpost.LastHeartbeatAt, outpost.UpdatedAt = &now, &now, now
	outpost.AppliedConfigurationVersion = request.ConfigurationVersion
	outpost.ConfigurationExpiresAt = request.ConfigurationExpiresAt
	outpost.RuntimeStatus = runtimeStatusFromLegacyOutpostStatus(outpost.Status)
	outpost.RuntimeReason = strings.TrimSpace(request.ErrorCode)
	outpost.RuntimeVersion = strings.TrimSpace(request.RuntimeVersion)
	outpost.Version = outpost.RuntimeVersion
	outpost.Metadata = nil
	if err := s.recordOutpostControlObservation(ctx, outpost, request); err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	if _, err := s.repo.RecordOutpostHeartbeat(ctx, outpost); err != nil {
		return sohaapi.IdentityOutpostHeartbeat{}, err
	}
	return sohaapi.IdentityOutpostHeartbeat{Accepted: true, DesiredConfigurationVersion: desired}, nil
}

func (s *Service) CheckIdentityOutpostAccess(ctx context.Context, outpostID, token string, request sohaapi.IdentityOutpostAccessCheckRequest) (sohaapi.IdentityOutpostAccessCheck, error) {
	if err := s.requireOutpostRuntimeSigner(); err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	outpost, err := s.authenticateOutpost(ctx, outpostID, token)
	if err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	providers, err := s.outpostProxyProviders(ctx, outpostID)
	if err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	version, err := s.resolveOutpostConfigurationVersion(ctx, outpostID, providers)
	if err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	if request.ConfigurationVersion != version {
		return sohaapi.IdentityOutpostAccessCheck{}, fmt.Errorf("%w: stale outpost configuration", apperrors.ErrConflict)
	}
	leaseRepository, err := s.outpostLeaseRepository()
	if err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	if err := leaseRepository.ValidateOutpostRuntimeLease(ctx, outpostID, outpost.TokenHash, version, nil); err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	result, err := s.checkOutpost(ctx, outpostID, domainprovider.OutpostCheckInput{SourceIP: request.SourceIP, ProviderID: request.ProviderID, OriginalURL: request.OriginalURL, RequestHost: request.RequestHost, RequestPath: request.RequestPath, Method: request.Method, SessionToken: request.SessionToken})
	if err != nil {
		return sohaapi.IdentityOutpostAccessCheck{}, err
	}
	item := sohaapi.IdentityOutpostAccessCheck{Decision: sohaapi.IdentityOutpostAccessCheckDecisionDeny, Reason: result.Reason, Headers: result.Headers}
	switch result.Decision {
	case domainprovider.ProxyDecisionAllow:
		item.StatusCode, item.Decision = http.StatusOK, sohaapi.IdentityOutpostAccessCheckDecisionAllow
	case domainprovider.ProxyDecisionLogin:
		item.StatusCode, item.Decision, item.RedirectURL = http.StatusFound, sohaapi.IdentityOutpostAccessCheckDecisionRedirect, result.LoginURL
	default:
		item.StatusCode = http.StatusForbidden
	}
	return item, nil
}

func (s *Service) RecordIdentityOutpostRuntimeEvents(ctx context.Context, outpostID, token string, request sohaapi.IdentityOutpostEventBatchRequest) (sohaapi.OperationStatus, error) {
	if err := s.requireOutpostRuntimeSigner(); err != nil {
		return sohaapi.OperationStatus{}, err
	}
	if strings.TrimSpace(request.AgentID) != strings.TrimSpace(outpostID) {
		return sohaapi.OperationStatus{}, fmt.Errorf("%w: outpost agent ID mismatch", apperrors.ErrAccessDenied)
	}
	events := make([]domainprovider.OutpostEvent, 0, len(request.Events))
	for _, event := range request.Events {
		occurredAt := event.OccurredAt
		events = append(events, domainprovider.OutpostEvent{EventType: string(event.Type), Result: event.Code, Reason: event.Message, Metadata: map[string]any{"configurationVersion": event.ConfigurationVersion, "eventId": event.ID}, CreatedAt: &occurredAt})
	}
	if _, err := s.RecordOutpostEvents(ctx, outpostID, domainprovider.OutpostEventsInput{Token: token, Events: events}); err != nil {
		return sohaapi.OperationStatus{}, err
	}
	return sohaapi.OperationStatus{Status: "accepted"}, nil
}

func (s *Service) outpostRuntimeConfig(ctx context.Context, outpost domainprovider.Outpost, providers []domainprovider.Provider) (sohaapi.IdentityOutpostRuntimeConfig, error) {
	if err := s.requireOutpostRuntimeSigner(); err != nil {
		return sohaapi.IdentityOutpostRuntimeConfig{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	version, err := s.resolveOutpostConfigurationVersion(ctx, outpost.ID, providers)
	if err != nil {
		return sohaapi.IdentityOutpostRuntimeConfig{}, err
	}
	config := sohaapi.IdentityOutpostRuntimeConfig{OutpostID: outpost.ID, ProtocolVersion: outpostProtocolVersion, ConfigurationVersion: version, IssuedAt: now, ExpiresAt: now.Add(outpostConfigurationLeaseTTL), KeyID: s.outpostSigningKeyID, CheckURL: "/identity/outposts/" + outpost.ID + "/check", Routes: outpostRoutes(providers)}
	payload, err := outpostConfigPayload(config)
	if err != nil {
		return sohaapi.IdentityOutpostRuntimeConfig{}, err
	}
	config.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.outpostSigningKey, payload))
	leaseRepository, err := s.outpostLeaseRepository()
	if err != nil {
		return sohaapi.IdentityOutpostRuntimeConfig{}, err
	}
	digest, err := outpostConfigurationDigest(providers)
	if err != nil {
		return sohaapi.IdentityOutpostRuntimeConfig{}, err
	}
	if err := leaseRepository.RecordOutpostRuntimeLease(ctx, outpost.ID, outpost.TokenHash, digest, version, config.IssuedAt, config.ExpiresAt); err != nil {
		return sohaapi.IdentityOutpostRuntimeConfig{}, err
	}
	return config, nil
}

func (s *Service) requireOutpostRuntimeSigner() error {
	if len(s.outpostSigningKey) != ed25519.PrivateKeySize || strings.TrimSpace(s.outpostSigningKeyID) == "" {
		return fmt.Errorf("%w: Outpost signing key is not configured", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func outpostRoutes(providers []domainprovider.Provider) []sohaapi.IdentityOutpostRoute {
	routes := make([]sohaapi.IdentityOutpostRoute, 0)
	for _, provider := range providers {
		for _, host := range compactStrings(configStringSlice(provider.Config, "externalHosts", "external_hosts", "hosts")) {
			routes = append(routes, sohaapi.IdentityOutpostRoute{ApplicationID: provider.ApplicationID, Host: host, PathPrefix: proxyPathPrefix(provider), ProviderID: provider.ID, SkipPaths: configStringSlice(provider.Config, "skipAuthPaths", "skip_auth_paths")})
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Host == routes[j].Host {
			return routes[i].PathPrefix < routes[j].PathPrefix
		}
		return routes[i].Host < routes[j].Host
	})
	return routes
}

func outpostConfigurationDigest(providers []domainprovider.Provider) (string, error) {
	routes := outpostRoutes(providers)
	payload, err := json.Marshal(routes)
	if err != nil {
		return "", fmt.Errorf("marshal outpost routes: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) resolveOutpostConfigurationVersion(ctx context.Context, outpostID string, providers []domainprovider.Provider) (int64, error) {
	repository, ok := s.repo.(outpostRuntimeVersionRepository)
	if !ok {
		return 0, fmt.Errorf("%w: Outpost runtime version repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	digest, err := outpostConfigurationDigest(providers)
	if err != nil {
		return 0, err
	}
	return repository.ResolveOutpostRuntimeVersion(ctx, outpostID, digest)
}

func outpostConfigPayload(config sohaapi.IdentityOutpostRuntimeConfig) ([]byte, error) {
	payload := struct {
		OutpostID            string                         `json:"outpostId"`
		ProtocolVersion      string                         `json:"protocolVersion"`
		ConfigurationVersion int64                          `json:"configurationVersion"`
		IssuedAt             time.Time                      `json:"issuedAt"`
		ExpiresAt            time.Time                      `json:"expiresAt"`
		KeyID                string                         `json:"keyId"`
		CheckURL             string                         `json:"checkUrl"`
		Routes               []sohaapi.IdentityOutpostRoute `json:"routes"`
	}{config.OutpostID, config.ProtocolVersion, config.ConfigurationVersion, config.IssuedAt, config.ExpiresAt, config.KeyID, config.CheckURL, config.Routes}
	return json.Marshal(payload)
}

func validateOutpostHeartbeatReport(request sohaapi.IdentityOutpostHeartbeatRequest, desired int64, now time.Time) error {
	if request.ConfigurationVersion > desired {
		return fmt.Errorf("%w: future outpost configuration", apperrors.ErrConflict)
	}
	if request.ConfigurationVersion == 0 {
		if request.ConfigurationExpiresAt != nil || request.Status == sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy {
			return fmt.Errorf("%w: outpost configuration is not applied", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	if request.ConfigurationExpiresAt == nil || !request.ConfigurationExpiresAt.After(now) || request.ConfigurationExpiresAt.After(now.Add(outpostConfigurationLeaseTTL)) {
		return fmt.Errorf("%w: invalid outpost configuration lease", apperrors.ErrConflict)
	}
	return nil
}
