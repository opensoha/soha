package identityprovider

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	outpostHeartbeatStaleAfter = 45 * time.Second
	outpostHeartbeatTimeout    = 90 * time.Second
)

func (s *Service) outpostManagementView(item domainprovider.Outpost, now time.Time) domainprovider.Outpost {
	item.RuntimeStatus, item.RuntimeReason = outpostRuntimeHealth(item, now, s.requireOutpostRuntimeSigner() == nil)
	if item.Mode != domainprovider.OutpostModeEmbedded {
		item.Deployment = &domainprovider.OutpostDeployment{ProtocolVersion: outpostProtocolVersion}
		if s.publicAccessURL != nil {
			item.Deployment.ControlPlaneURL, _ = normalizeOutpostForwardAuthURL(s.publicAccessURL())
		}
		if s.requireOutpostRuntimeSigner() == nil {
			item.Deployment.TrustKeyID = s.outpostSigningKeyID
			item.Deployment.TrustPublicKey = base64.StdEncoding.EncodeToString(s.outpostSigningKey[ed25519.SeedSize:])
		}
	}
	return item
}

func outpostRuntimeHealth(item domainprovider.Outpost, now time.Time, signingReady bool) (string, string) {
	if item.Mode == domainprovider.OutpostModeEmbedded {
		return "available", "embedded_runtime"
	}
	if !signingReady {
		return "unavailable", "signing_key_unconfigured"
	}
	if item.ClaimedAgentID == "" {
		return "unavailable", "awaiting_registration"
	}
	if item.ProtocolVersion != "" && item.ProtocolVersion != outpostProtocolVersion {
		return "unavailable", "protocol_incompatible"
	}
	if item.LastHeartbeatAt == nil {
		return "unavailable", "awaiting_heartbeat"
	}
	if now.Sub(*item.LastHeartbeatAt) > outpostHeartbeatTimeout {
		return "unavailable", "heartbeat_timeout"
	}
	if item.ProtocolVersion == "" {
		return "degraded", "legacy_runtime"
	}
	if item.RuntimeStatus == "unavailable" && item.RuntimeReason != "" {
		return "unavailable", item.RuntimeReason
	}
	if item.ConfigurationVersion == 0 || item.AppliedConfigurationVersion == 0 {
		return "unavailable", "configuration_not_applied"
	}
	if item.ConfigurationExpiresAt != nil && !item.ConfigurationExpiresAt.After(now) {
		return "unavailable", "configuration_expired"
	}
	return outpostReportedHealth(item, now)
}

func outpostReportedHealth(item domainprovider.Outpost, now time.Time) (string, string) {
	if item.RuntimeStatus != "available" {
		status := "unavailable"
		if item.RuntimeStatus == "degraded" {
			status = "degraded"
		}
		reason := item.RuntimeReason
		if reason == "" {
			reason = "runtime_" + status
		}
		return status, reason
	}
	if item.AppliedConfigurationVersion != item.ConfigurationVersion {
		return "degraded", "configuration_pending"
	}
	if now.Sub(*item.LastHeartbeatAt) > outpostHeartbeatStaleAfter {
		return "degraded", "heartbeat_stale"
	}
	if item.ConfigurationExpiresAt == nil {
		return "degraded", "configuration_expiry_unknown"
	}
	return "available", ""
}

func runtimeStatusFromLegacyOutpostStatus(status string) string {
	switch status {
	case domainprovider.OutpostStatusOnline:
		return "available"
	case domainprovider.OutpostStatusDegraded:
		return "degraded"
	default:
		return "unavailable"
	}
}

func normalizeOutpostForwardAuthURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || u.Hostname() == "" ||
		(u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: forward-auth URL must be an absolute HTTP(S) URL without credentials, query or fragment", apperrors.ErrInvalidArgument)
	}
	return u.String(), nil
}
