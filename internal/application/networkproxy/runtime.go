package networkproxy

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	domainruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

var hashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type RuntimeStore interface {
	Get(context.Context, string) (domain.Instance, error)
	Touch(context.Context, string, time.Time) error
	Apply(context.Context, string, domain.Applied, time.Time) error
	Observe(context.Context, string, time.Time, domain.Observation) error
	PutConnections(context.Context, string, time.Time, []domain.Connection) error
	NextClose(context.Context, string, time.Time) (*domain.CloseCommand, error)
	CompleteClose(context.Context, string, string, string, string, time.Time) error
	Cleanup(context.Context, time.Time) error
}

type CredentialStore interface {
	ActiveCredential(context.Context, string, time.Time) (domainruntime.Credential, error)
}

type Runtime struct {
	store       RuntimeStore
	credentials CredentialStore
	keys        keyring.Ring
	now         func() time.Time
}

func NewRuntime(store RuntimeStore, credentials CredentialStore, keys keyring.Ring) (*Runtime, error) {
	if missing(store) || missing(credentials) || keys.Active().ID() == "" {
		return nil, fmt.Errorf("proxy runtime: store, credentials and encryption key are required")
	}
	return &Runtime{store: store, credentials: credentials, keys: keys, now: time.Now}, nil
}

func (s *Runtime) authenticate(ctx context.Context, identity networkidentity.Identity) (domain.Instance, error) {
	if identity.Scope != networkidentity.ScopeNetworkControl || identity.Kind != "proxy" || !identifier.MatchString(identity.ID) {
		return domain.Instance{}, apperrors.ErrUnauthorized
	}
	credential, err := s.credentials.ActiveCredential(ctx, identity.CertificateFingerprint, s.now().UTC())
	if err != nil {
		return domain.Instance{}, err
	}
	if credential.RuntimeID != identity.ID || credential.RuntimeKind != "proxy" || credential.CertificateFingerprint != identity.CertificateFingerprint {
		return domain.Instance{}, apperrors.ErrUnauthorized
	}
	return s.store.Get(ctx, identity.ID)
}

func (s *Runtime) Configuration(ctx context.Context, identity networkidentity.Identity) (*domain.Configuration, error) {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return nil, err
	}
	if err := s.store.Touch(ctx, item.ID, s.now().UTC()); err != nil {
		return nil, err
	}
	if item.DesiredRevision == 0 {
		return nil, nil
	}
	content, err := secretcrypto.DecryptStringWithKeyring(s.keys, item.DesiredContentEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt proxy configuration: %w", err)
	}
	return &domain.Configuration{Revision: item.DesiredRevision, Engine: item.Engine, Enabled: item.Enabled,
		ContentHash: item.DesiredHash, Content: content, ValidUntil: s.now().UTC().Add(5 * time.Minute)}, nil
}

func (s *Runtime) Applied(ctx context.Context, identity networkidentity.Identity, applied domain.Applied) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	if applied.Revision < 1 || applied.Revision != item.DesiredRevision || !hashPattern.MatchString(applied.ReadbackHash) ||
		(applied.Status != "applied" && applied.Status != "rejected" && applied.Status != "rolled-back") || len(applied.ReasonCode) > 128 {
		return invalid("proxy apply acknowledgement is invalid")
	}
	return s.store.Apply(ctx, item.ID, applied, s.now().UTC())
}

func (s *Runtime) Observe(ctx context.Context, identity networkidentity.Identity, at time.Time, observation domain.Observation) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if err := validateObservation(item, now, at, observation); err != nil {
		return err
	}
	return s.store.Observe(ctx, item.ID, at, observation)
}

func validateObservation(item domain.Instance, now, at time.Time, observation domain.Observation) error {
	if at.Before(now.Add(-2*time.Minute)) || at.After(now.Add(time.Minute)) || observation.Engine != item.Engine ||
		len(observation.EngineVersion) == 0 || len(observation.EngineVersion) > 64 || observation.UptimeSeconds < 0 ||
		observation.UploadTotal < 0 || observation.DownloadTotal < 0 || len(observation.ReasonCode) > 128 ||
		(observation.ActiveConnections != nil && (*observation.ActiveConnections < 0 || *observation.ActiveConnections > 1000000)) ||
		(observation.Health != "healthy" && observation.Health != "degraded" && observation.Health != "unhealthy") {
		return invalid("proxy observation is invalid")
	}
	if item.Engine == domain.EngineV2Ray && observation.ActiveConnections != nil {
		return invalid("V2Ray connection count is unsupported")
	}
	return validateCapabilities(item.Engine, observation.Capabilities)
}

func validateCapabilities(engine string, capabilities []string) error {
	if capabilities == nil || len(capabilities) > 3 {
		return invalid("proxy capabilities are invalid")
	}
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if capability != "traffic" && capability != "connections" && capability != "close_connection" {
			return invalid("proxy capabilities are invalid")
		}
		if _, duplicate := seen[capability]; duplicate {
			return invalid("proxy capabilities must be unique")
		}
		seen[capability] = struct{}{}
	}
	if engine == domain.EngineV2Ray && (slices.Contains(capabilities, "connections") || slices.Contains(capabilities, "close_connection")) {
		return invalid("V2Ray connection operations are unsupported")
	}
	if slices.Contains(capabilities, "close_connection") && !slices.Contains(capabilities, "connections") {
		return invalid("proxy close capability requires connections")
	}
	return nil
}

func (s *Runtime) PutConnections(ctx context.Context, identity networkidentity.Identity, at time.Time, engine string, connections []domain.Connection) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if item.Engine == domain.EngineV2Ray || engine != item.Engine || at.Before(now.Add(-2*time.Minute)) || at.After(now.Add(time.Minute)) || len(connections) > 200 {
		return invalid("proxy connection snapshot is unsupported or invalid")
	}
	seen := make(map[string]struct{}, len(connections))
	for _, connection := range connections {
		if !identifier.MatchString(connection.ID) || len(connection.Destination) < 1 || len(connection.Destination) > 253 ||
			(connection.Network != "tcp" && connection.Network != "udp" && connection.Network != "other") ||
			connection.UploadBytes < 0 || connection.DownloadBytes < 0 {
			return invalid("proxy connection is invalid")
		}
		if _, duplicate := seen[connection.ID]; duplicate {
			return invalid("proxy connection IDs must be unique")
		}
		seen[connection.ID] = struct{}{}
	}
	if err := s.store.PutConnections(ctx, item.ID, at, connections); err != nil {
		return err
	}
	return s.store.Touch(ctx, item.ID, now)
}

func (s *Runtime) NextClose(ctx context.Context, identity networkidentity.Identity) (*domain.CloseCommand, error) {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return nil, err
	}
	if item.Engine == domain.EngineV2Ray {
		return nil, apperrors.ErrConflict
	}
	if err := s.store.Touch(ctx, item.ID, s.now().UTC()); err != nil {
		return nil, err
	}
	return s.store.NextClose(ctx, item.ID, s.now().UTC())
}

func (s *Runtime) CompleteClose(ctx context.Context, identity networkidentity.Identity, commandID, status, reason string) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	if !identifier.MatchString(commandID) || (status != "closed" && status != "not-found" && status != "failed") || len(reason) > 128 || strings.TrimSpace(reason) != reason {
		return invalid("proxy close result is invalid")
	}
	return s.store.CompleteClose(ctx, item.ID, commandID, status, reason, s.now().UTC())
}

func (s *Runtime) Cleanup(ctx context.Context) error {
	return s.store.Cleanup(ctx, s.now().UTC())
}
