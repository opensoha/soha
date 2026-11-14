package networkproxy

import (
	"context"
	"fmt"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
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
	GrantConfigurationLease(context.Context, string, int64, string, time.Time, time.Time) error
	PrepareConfiguration(context.Context, string, time.Time) (domain.Instance, error)
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

type PublicationReader interface {
	ServerPublication(context.Context, string, string, int64, time.Time) (api.NetworkProxyServerPublication, error)
}

type Runtime struct {
	publications PublicationReader
	store        RuntimeStore
	credentials  CredentialStore
	keys         keyring.Ring
	now          func() time.Time
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
	credential, err := s.credentials.ActiveCredential(ctx, identity.CertificateFingerprint, s.now().UTC())
	if err != nil {
		return nil, err
	}
	if !slices.Contains(credential.Capabilities, "proxy-policy-v1") || !slices.Contains(credential.Capabilities, "proxy-guarded-dns-v1") {
		return nil, nil
	}
	item, err = s.store.PrepareConfiguration(ctx, item.ID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	if item.DesiredRevision == 0 {
		return nil, nil
	}
	content, err := secretcrypto.DecryptStringWithKeyring(s.keys, item.DesiredContentEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt proxy configuration: %w", err)
	}
	validUntil := s.now().UTC().Add(5 * time.Minute)
	if credential.ExpiresAt.Before(validUntil) {
		validUntil = credential.ExpiresAt
	}
	desired := &domain.Configuration{EngineDescriptor: item.EngineDescriptor, Revision: item.DesiredRevision, Engine: item.Engine, Enabled: item.Enabled && !item.SecurityRestricted, Binding: item.Binding, Protection: item.Protection,
		ContentHash: item.DesiredHash, Content: content, ValidUntil: validUntil}
	if err := s.attachPublication(ctx, item, desired); err != nil {
		return nil, err
	}
	if err := s.store.GrantConfigurationLease(ctx, item.ID, desired.Revision, desired.Digest(), desired.ValidUntil, s.now().UTC()); err != nil {
		return nil, err
	}
	return desired, nil
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
	if observation.Health == "healthy" {
		if !slices.Contains(observation.Capabilities, "protected_destinations_v1") || !slices.Contains(observation.Capabilities, "guarded_dns_tcp_v1") {
			return invalid("healthy proxy observations require protected destination support")
		}
		item, err = s.store.PrepareConfiguration(ctx, item.ID, s.now().UTC())
		if err != nil {
			return err
		}
		if !item.Enabled || item.SecurityRestricted || item.PolicyVersion < 1 || item.DesiredRevision != item.ObservedRevision || item.PolicyVersion != item.ObservedPolicyVersion {
			return apperrors.ErrConflict
		}
	}
	now := s.now().UTC()
	if err := validateObservation(item, now, at, observation); err != nil {
		return err
	}
	return s.store.Observe(ctx, item.ID, at, observation)
}

func validateObservation(item domain.Instance, now, at time.Time, observation domain.Observation) error {
	if err := validateObservationValues(item, now, at, observation); err != nil {
		return err
	}
	if item.Engine == domain.EngineV2Ray && observation.ActiveConnections != nil {
		return invalid("V2Ray connection count is unsupported")
	}
	if observation.ProcessEpoch != "" && (!identifier.MatchString(observation.ProcessEpoch) || observation.SampleSequence < 1 || observation.SampleSequence > 9007199254740991) {
		return invalid("invalid sample epoch or sequence")
	}
	if item.ProcessEpoch != "" && observation.ProcessEpoch == "" {
		return invalid("sample epoch required")
	}
	if item.Engine == domain.EngineExternal && item.EngineDescriptor != nil && observation.EngineDescriptor != nil && (item.EngineDescriptor.EngineID != observation.EngineDescriptor.EngineID || item.EngineDescriptor.AdapterID != observation.EngineDescriptor.AdapterID) {
		return invalid("engine descriptor identity mismatch")
	}
	if err := validateDescriptor(item.Engine, observation.EngineDescriptor); err != nil {
		return err
	}
	return validateCapabilities(item.Engine, observation.Capabilities)
}

func validateCapabilities(engine string, capabilities []string) error {
	if capabilities == nil || len(capabilities) > 16 {
		return invalid("proxy capabilities are invalid")
	}
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !slices.Contains([]string{"groups", "select", "probe", "refresh", "rules", "dns", "logs", "diagnostics", "traffic", "connections", "close_connection", "protected_destinations_v1", "protection_rules_readback", "guarded_dns_tcp_v1", "guarded_udp_v1"}, capability) {
			return invalid("proxy capabilities are invalid")
		}
		if _, duplicate := seen[capability]; duplicate {
			return invalid("proxy capabilities must be unique")
		}
		seen[capability] = struct{}{}
	}
	if slices.Contains(capabilities, "protection_rules_readback") && ((engine != domain.EngineMihomo && engine != domain.EngineExternal) || !slices.Contains(capabilities, "protected_destinations_v1")) {
		return invalid("proxy rule readback is unsupported")
	}
	if engine == domain.EngineV2Ray && (slices.Contains(capabilities, "connections") || slices.Contains(capabilities, "close_connection")) {
		return invalid("V2Ray connection operations are unsupported")
	}
	if slices.Contains(capabilities, "close_connection") && !slices.Contains(capabilities, "connections") {
		return invalid("proxy close capability requires connections")
	}
	return validateGuardedUDPCapability(engine, capabilities)
}

func validateGuardedUDPCapability(engine string, capabilities []string) error {
	if !slices.Contains(capabilities, "guarded_udp_v1") {
		return nil
	}
	if (engine != domain.EngineMihomo && engine != domain.EngineExternal) || !slices.Contains(capabilities, "protected_destinations_v1") || !slices.Contains(capabilities, "guarded_dns_tcp_v1") {
		return invalid("guarded UDP requires a supported engine, protection and guarded DNS")
	}
	return nil
}

func (s *Runtime) PutConnections(ctx context.Context, identity networkidentity.Identity, at time.Time, engine string, connections []domain.Connection) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if item.ProcessEpoch != "" || item.Engine == domain.EngineV2Ray || engine != item.Engine || at.Before(now.Add(-2*time.Minute)) || at.After(now.Add(time.Minute)) || len(connections) > 200 {
		return invalid("proxy connection snapshot is unsupported or invalid")
	}
	if err := validateConnections(connections); err != nil {
		return err
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

// PutConnectionsSnapshot preserves epoch and cardinality independently from the bounded list.
func (s *Runtime) PutConnectionsSnapshot(ctx context.Context, identity networkidentity.Identity, at time.Time, engine string, snapshot domain.ConnectionsSnapshot) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	if item.Engine == domain.EngineV2Ray || !slices.Contains(item.Capabilities, "connections") || item.ProcessEpoch != snapshot.ProcessEpoch || !identifier.MatchString(snapshot.ProcessEpoch) || snapshot.SampleSequence < 1 || len(snapshot.Connections) > 200 || snapshot.TotalActive == nil || *snapshot.TotalActive < len(snapshot.Connections) || *snapshot.TotalActive > 1000000 || engine != item.Engine || at.Before(s.now().Add(-2*time.Minute)) || at.After(s.now().Add(time.Minute)) {
		return invalid("invalid connection snapshot")
	}
	if snapshot.ReturnedCount != nil && *snapshot.ReturnedCount != len(snapshot.Connections) || snapshot.Truncated != nil && *snapshot.Truncated != (*snapshot.TotalActive > len(snapshot.Connections)) {
		return invalid("inconsistent snapshot counts")
	}
	if err := validateConnections(snapshot.Connections); err != nil {
		return err
	}
	store, ok := s.store.(interface {
		PutConnectionsSnapshot(context.Context, string, time.Time, domain.ConnectionsSnapshot) error
	})
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	return store.PutConnectionsSnapshot(ctx, item.ID, at, snapshot)
}
func validateConnections(connections []domain.Connection) error {
	seen := map[string]bool{}
	for _, c := range connections {
		if !identifier.MatchString(c.ID) || seen[c.ID] || len(c.Destination) < 1 || len(c.Destination) > 253 || c.UploadBytes < 0 || c.DownloadBytes < 0 || !slices.Contains([]string{"tcp", "udp", "other"}, c.Network) {
			return invalid("invalid connection")
		}
		seen[c.ID] = true
	}
	return nil
}
func (s *Runtime) CompleteCloseWithEpoch(ctx context.Context, identity networkidentity.Identity, commandID, epoch, status, reason string) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	if epoch == "" {
		return s.CompleteClose(ctx, identity, commandID, status, reason)
	}
	if item.ProcessEpoch != epoch || !identifier.MatchString(commandID) || !slices.Contains([]string{"closed", "not-found", "failed"}, status) || len(reason) > 128 {
		return invalid("invalid close result")
	}
	store, ok := s.store.(EpochCloseStore)
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	return store.CompleteCloseEpoch(ctx, item.ID, commandID, epoch, status, reason, s.now().UTC())
}

func validateObservationValues(item domain.Instance, now, at time.Time, observation domain.Observation) error {
	if at.Before(now.Add(-2*time.Minute)) || at.After(now.Add(time.Minute)) || observation.Engine != item.Engine ||
		len(observation.EngineVersion) == 0 || len(observation.EngineVersion) > 64 || observation.UptimeSeconds < 0 ||
		observation.UploadTotal < 0 || observation.DownloadTotal < 0 || len(observation.ReasonCode) > 128 ||
		(observation.ActiveConnections != nil && (*observation.ActiveConnections < 0 || *observation.ActiveConnections > 1000000)) ||
		(observation.Health != "healthy" && observation.Health != "degraded" && observation.Health != "unhealthy") {
		return invalid("proxy observation is invalid")
	}
	return nil
}

func (s *Runtime) SetPublicationReader(reader PublicationReader) { s.publications = reader }
func (s *Runtime) attachPublication(ctx context.Context, item domain.Instance, desired *domain.Configuration) error {
	if item.BundleID == "" {
		return nil
	}
	if missing(s.publications) {
		return apperrors.ErrServiceUnavailable
	}
	publication, err := s.publications.ServerPublication(ctx, item.ID, item.BundleID, item.BundleRevision, desired.ValidUntil)
	if err != nil {
		return apperrors.ErrServiceUnavailable
	}
	if publication.BundleID != item.BundleID || publication.Revision != item.BundleRevision || publication.ServerHash != desired.ContentHash || !publication.ValidUntil.After(s.now().UTC()) {
		return apperrors.ErrServiceUnavailable
	}
	if publication.ValidUntil.Before(desired.ValidUntil) {
		desired.ValidUntil = publication.ValidUntil
	}
	publication.ValidUntil = desired.ValidUntil
	desired.Publication = &publication
	return nil
}
