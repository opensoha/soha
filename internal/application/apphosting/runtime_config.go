package apphosting

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const (
	defaultDataWarningBytes = domain.DefaultAppDataWarningMiB << 20
	maxEnvironmentEntries   = 32
)

var runtimeEnvironmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type runtimeConfigRepository interface {
	GetRuntimeConfig(context.Context, string) (domain.RuntimeConfig, error)
	MutateRuntimeConfig(context.Context, string, func(domain.App, domain.RuntimeConfig) (domain.RuntimeConfig, error)) (domain.RuntimeConfig, error)
}

func WithRuntimeConfigKey(key []byte) Option {
	return func(s *Service) { s.runtimeConfigKey = slices.Clone(key) }
}

func (s *Service) GetRuntimeConfig(ctx context.Context, actor identity.Principal, id string) (sohaapi.HostedAppRuntimeConfig, error) {
	app, _, err := s.loadAuthorizedAppRead(ctx, actor, id)
	if err != nil {
		return sohaapi.HostedAppRuntimeConfig{}, err
	}
	repo, ok := s.repo.(runtimeConfigRepository)
	if !ok || app.Kind != sohaapi.HostedAppKindDynamic {
		return sohaapi.HostedAppRuntimeConfig{}, apperrors.ErrNotFound
	}
	if !app.IsOwner(actor.UserID) && s.authorize(ctx, actor, access.PermAppHostingAdminView) != nil &&
		s.authorize(ctx, actor, access.PermAppHostingAdminManage) != nil {
		return sohaapi.HostedAppRuntimeConfig{}, apperrors.ErrAccessDenied
	}
	cfg, err := repo.GetRuntimeConfig(ctx, app.ID)
	return runtimeConfigDTO(cfg, app.IsOwner(actor.UserID)), err
}

func (s *Service) UpdateRuntimeConfig(ctx context.Context, actor identity.Principal, id string, req sohaapi.UpdateHostedAppRuntimeConfigRequest) (sohaapi.HostedAppRuntimeConfig, error) {
	repo, ok := s.repo.(runtimeConfigRepository)
	if !ok {
		return sohaapi.HostedAppRuntimeConfig{}, apperrors.ErrServiceUnavailable
	}
	id, err := validateIdentifier(id, "appId")
	if err != nil {
		return sohaapi.HostedAppRuntimeConfig{}, err
	}
	writeEnvironment := len(req.SetEnvironment) > 0 || len(req.RemoveEnvironment) > 0
	writeOwnerOnly := writeEnvironment || req.SensitivePublisherIDs != nil
	canPublish := s.authorize(ctx, actor, access.PermAppHostingAppsPublish) == nil
	canManage := s.authorize(ctx, actor, access.PermAppHostingAdminManage) == nil
	if (!canPublish && !canManage) || (writeOwnerOnly && !canPublish) {
		return sohaapi.HostedAppRuntimeConfig{}, apperrors.ErrAccessDenied
	}
	encrypted, err := s.encryptEnvironmentUpdate(id, req)
	if err != nil {
		return sohaapi.HostedAppRuntimeConfig{}, err
	}
	var owner bool
	cfg, err := repo.MutateRuntimeConfig(ctx, id, func(app domain.App, cfg domain.RuntimeConfig) (domain.RuntimeConfig, error) {
		owner = app.IsOwner(actor.UserID) && canPublish
		if app.Kind != sohaapi.HostedAppKindDynamic || app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
			return cfg, apperrors.ErrConflict
		}
		if (!owner && !canManage) || (writeOwnerOnly && !owner) {
			return cfg, apperrors.ErrAccessDenied
		}
		if err := applySensitivePublisherConsent(app, &cfg, req); err != nil {
			return cfg, err
		}
		return applyRuntimeConfigUpdate(cfg, req, encrypted)
	})
	if err != nil {
		return sohaapi.HostedAppRuntimeConfig{}, err
	}
	s.recordAudit(ctx, actor, "runtime.config.update", "success", id, "Updated hosted runtime configuration", map[string]any{
		"setCount": len(encrypted), "removedCount": len(req.RemoveEnvironment), "warningThresholdChanged": req.DataWarningBytes != nil,
		"maintainerConsentChanged": req.SensitivePublisherIDs != nil,
	})
	return runtimeConfigDTO(cfg, writeOwnerOnly && owner), nil
}

func (s *Service) encryptEnvironmentUpdate(appID string, req sohaapi.UpdateHostedAppRuntimeConfigRequest) (map[string]string, error) {
	if len(req.SetEnvironment) > maxEnvironmentEntries || len(req.RemoveEnvironment) > maxEnvironmentEntries ||
		(req.DataWarningBytes != nil && (*req.DataWarningBytes < 1 || *req.DataWarningBytes > 1<<40)) ||
		(len(req.SetEnvironment) == 0 && len(req.RemoveEnvironment) == 0 && req.DataWarningBytes == nil && req.SensitivePublisherIDs == nil) {
		return nil, apperrors.ErrInvalidArgument
	}
	if len(req.SetEnvironment) > 0 && len(s.runtimeConfigKey) < 32 {
		return nil, apperrors.ErrServiceUnavailable
	}
	sealed := make(map[string]string, len(req.SetEnvironment))
	for _, item := range req.SetEnvironment {
		if !validRuntimeEnvironmentName(item.Name) || item.Value == "" || len(item.Value) > 4096 ||
			!utf8.ValidString(item.Value) || strings.ContainsRune(item.Value, 0) {
			return nil, apperrors.ErrInvalidArgument
		}
		if _, exists := sealed[item.Name]; exists {
			return nil, apperrors.ErrInvalidArgument
		}
		value, err := secretcrypto.EncryptString(runtimeEnvironmentKey(s.runtimeConfigKey, appID, item.Name), "v1:"+item.Value)
		if err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		sealed[item.Name] = value
	}
	return sealed, nil
}

func applySensitivePublisherConsent(app domain.App, cfg *domain.RuntimeConfig, req sohaapi.UpdateHostedAppRuntimeConfigRequest) error {
	ids := req.SensitivePublisherIDs
	if ids == nil {
		return nil
	}
	if len(ids) > 32 || (len(ids) > 0 && !req.AcknowledgeSensitiveAccess) {
		return apperrors.ErrInvalidArgument
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] || !slices.Contains(app.MaintainerUserIDs, id) {
			return apperrors.ErrInvalidArgument
		}
		seen[id] = true
	}
	cfg.SensitivePublisherIDs = slices.Clone(ids)
	cfg.SensitivePublisherOwnerID = app.OwnerUserID
	return nil
}

func applyRuntimeConfigUpdate(cfg domain.RuntimeConfig, req sohaapi.UpdateHostedAppRuntimeConfigRequest, sealed map[string]string) (domain.RuntimeConfig, error) {
	if cfg.Environment == nil {
		cfg.Environment = make(map[string]string)
	}
	for _, name := range req.RemoveEnvironment {
		if !validRuntimeEnvironmentName(name) || sealed[name] != "" {
			return cfg, apperrors.ErrInvalidArgument
		}
		delete(cfg.Environment, name)
	}
	maps.Copy(cfg.Environment, sealed)
	if len(cfg.Environment) > maxEnvironmentEntries {
		return cfg, apperrors.ErrInvalidArgument
	}
	if req.DataWarningBytes != nil {
		cfg.DataWarningBytes = *req.DataWarningBytes
	}
	return cfg, nil
}

func validRuntimeEnvironmentName(name string) bool {
	if !runtimeEnvironmentName.MatchString(name) {
		return false
	}
	switch name {
	case "DATABASE_URL", "HOME", "HOSTNAME", "PATH", "PORT", "TMPDIR", "NODE_OPTIONS", "SSL_CERT_FILE", "SSL_CERT_DIR":
		return false
	}
	for _, prefix := range []string{"SOHA_", "K_", "KUBERNETES_", "LD_", "PYTHON"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func runtimeConfigDTO(cfg domain.RuntimeConfig, includeKeys bool) sohaapi.HostedAppRuntimeConfig {
	keys := []string{}
	if includeKeys && len(cfg.Environment) > 0 {
		keys = slices.Sorted(maps.Keys(cfg.Environment))
	}
	out := sohaapi.HostedAppRuntimeConfig{EnvironmentKeys: keys, DataWarningBytes: dataWarningBytes(cfg)}
	if includeKeys {
		out.SensitivePublisherIDs = slices.Clone(cfg.SensitivePublisherIDs)
	}
	return out
}

func dataWarningBytes(cfg domain.RuntimeConfig) int64 {
	if cfg.DataWarningBytes > 0 {
		return cfg.DataWarningBytes
	}
	return defaultDataWarningBytes
}

func runtimeEnvironmentKey(key []byte, appID, name string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("soha-hosted-environment/v1\x00" + appID + "\x00" + name))
	return hex.EncodeToString(mac.Sum(nil))
}

// OpenRuntimeEnvironment is called only when provisioning a revision's private runtime Secret.
func OpenRuntimeEnvironment(key []byte, rev domain.Revision) (map[string][]byte, error) {
	values := make(map[string][]byte, len(rev.RuntimeEnvironment)+1)
	for name, sealed := range rev.RuntimeEnvironment {
		if len(key) < 32 || !validRuntimeEnvironmentName(name) || !secretcrypto.Encrypted(sealed) {
			return nil, errors.New("invalid encrypted hosted environment")
		}
		plain, err := secretcrypto.DecryptString(runtimeEnvironmentKey(key, rev.AppID, name), sealed)
		value, valid := strings.CutPrefix(plain, "v1:")
		if err != nil || !valid {
			return nil, errors.New("could not open encrypted hosted environment")
		}
		values[name] = []byte(value)
	}
	return values, nil
}
