package networkproxy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type ManagementStore interface {
	Create(context.Context, domain.InstanceInput, time.Time) (domain.Instance, error)
	Get(context.Context, string) (domain.Instance, error)
	List(context.Context, string, string, int, int) ([]domain.Instance, error)
	UpdateConfiguration(context.Context, string, int64, bool, string, string, *domain.Binding, bool, time.Time) (domain.Instance, error)
	ResolveBinding(context.Context, domain.Binding) (domain.Binding, error)
	Connections(context.Context, string) (domain.ConnectionsSnapshot, error)
	QueueClose(context.Context, string, string, string, time.Time) (domain.CloseCommand, error)
	GetClose(context.Context, string, string, time.Time) (domain.CloseCommand, error)
}

type TrafficReader interface {
	Samples(context.Context, string, time.Time, time.Time) ([]domain.TrafficSample, error)
}

type closeRequestStore interface {
	ReplayClose(context.Context, string, string, string, domain.CloseInput, time.Time) (domain.CloseCommand, bool, error)
	QueueCloseRequest(context.Context, string, string, string, domain.CloseInput, time.Time) (domain.CloseCommand, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}
type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Management struct {
	probes      ProbeProfiles
	store       ManagementStore
	traffic     TrafficReader
	permissions *appaccess.PermissionResolver
	scope       ScopeAuthorizer
	audit       AuditRecorder
	operations  OperationRecorder
	keys        keyring.Ring
	now         func() time.Time
}

func NewManagement(store ManagementStore, traffic TrafficReader, permissions *appaccess.PermissionResolver, scope ScopeAuthorizer, audit AuditRecorder, operations OperationRecorder, keys keyring.Ring) (*Management, error) {
	for name, value := range map[string]any{"store": store, "permissions": permissions, "scope": scope, "audit": audit, "operations": operations} {
		if missing(value) {
			return nil, fmt.Errorf("proxy management: %s is required", name)
		}
	}
	if keys.Active().ID() == "" {
		return nil, fmt.Errorf("proxy management: encryption key is required")
	}
	return &Management{store: store, traffic: traffic, permissions: permissions, scope: scope, audit: audit, operations: operations, keys: keys, now: time.Now}, nil
}

func missing(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func validEngine(engine string) bool {
	return engine == domain.EngineExternal || engine == domain.EngineMihomo || engine == domain.EngineSingBox || engine == domain.EngineV2Ray
}

func public(item domain.Instance, now time.Time) domain.Instance {
	if item.Capabilities == nil {
		item.Capabilities = []string{}
	}
	switch {
	case !item.Enabled:
		item.Status = "disabled"
	case !item.Registered:
		item.Status = "unregistered"
	case item.LastSeenAt == nil || now.Sub(*item.LastSeenAt) > 2*time.Minute:
		item.Status = "offline"
	case item.LastSampleAt != nil && now.Sub(*item.LastSampleAt) <= 2*time.Minute && item.Health == "healthy" && item.DesiredRevision == item.ObservedRevision && item.PolicyVersion > 0 && item.PolicyVersion == item.ObservedPolicyVersion && slices.Contains(item.Capabilities, "protected_destinations_v1") && slices.Contains(item.Capabilities, "guarded_dns_tcp_v1"):
		item.Status = "online"
	default:
		item.Status = "degraded"
	}
	return item
}

func (s *Management) Create(ctx context.Context, principal domainidentity.Principal, input domain.InstanceInput) (domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesCreate); err != nil {
		return domain.Instance{}, err
	}
	input.ID, input.Name, input.Engine, input.Host = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.Engine), strings.TrimSpace(input.Host)
	if err := validateDescriptor(input.Engine, input.EngineDescriptor); err != nil {
		return domain.Instance{}, err
	}
	if !identifier.MatchString(input.ID) || input.Name == "" || len(input.Name) > 128 || len(input.Host) > 128 || !validEngine(input.Engine) {
		return domain.Instance{}, invalid("proxy instance input is invalid")
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyInstancesCreate, domain.Instance{ID: input.ID, Engine: input.Engine, Host: input.Host}); err != nil {
		return domain.Instance{}, err
	}
	item, err := s.store.Create(ctx, input, s.now().UTC())
	if err != nil {
		return domain.Instance{}, err
	}
	s.record(ctx, principal, "network_access.proxy_instances.create", item.ID)
	return public(item, s.now().UTC()), nil
}

func (s *Management) List(ctx context.Context, principal domainidentity.Principal, search, engine string, limit int) ([]domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesView); err != nil {
		return nil, err
	}
	search, engine = strings.TrimSpace(search), strings.TrimSpace(engine)
	if len(search) > 200 || (engine != "" && !validEngine(engine)) || limit < 0 || limit > 200 {
		return nil, invalid("proxy instance filter is invalid")
	}
	if limit == 0 {
		limit = 100
	}
	items := make([]domain.Instance, 0, limit)
	for offset := 0; offset < 10000; offset += 200 {
		page, err := s.store.List(ctx, search, engine, 200, offset)
		if err != nil {
			return nil, err
		}
		for _, item := range page {
			if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyInstancesView, item); errors.Is(err, apperrors.ErrAccessDenied) {
				continue
			} else if err != nil {
				return nil, err
			}
			items = append(items, public(item, s.now().UTC()))
			if len(items) == limit {
				return items, nil
			}
		}
		if len(page) < 200 {
			return items, nil
		}
	}
	return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "proxy_scope_scan_limit", "Narrow the proxy instance query before retrying.", "请缩小代理实例查询范围后重试。")
}

func (s *Management) Get(ctx context.Context, principal domainidentity.Principal, id string) (domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesView); err != nil {
		return domain.Instance{}, err
	}
	if !identifier.MatchString(id) {
		return domain.Instance{}, invalid("proxy instance ID is invalid")
	}
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.Instance{}, err
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyInstancesView, item); err != nil {
		return domain.Instance{}, err
	}
	return public(item, s.now().UTC()), nil
}

func (s *Management) UpdateConfiguration(ctx context.Context, principal domainidentity.Principal, id string, input domain.ConfigurationInput) (domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesUpdate); err != nil {
		return domain.Instance{}, err
	}
	if err := validateConfigurationInput(id, input); err != nil {
		return domain.Instance{}, err
	}
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.Instance{}, err
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyInstancesUpdate, existing); err != nil {
		return domain.Instance{}, err
	}
	binding, err := s.configurationBinding(ctx, principal, input, existing)
	if err != nil {
		return domain.Instance{}, err
	}
	encrypted, hash, err := s.configurationContent(input, existing)
	if err != nil {
		return domain.Instance{}, err
	}
	item, err := s.store.UpdateConfiguration(ctx, id, input.ExpectedRevision, input.Enabled, encrypted, hash, binding, input.ClearBinding, s.now().UTC())
	if err != nil {
		return domain.Instance{}, err
	}
	s.record(ctx, principal, "network_access.proxy_instances.update", id)
	return public(item, s.now().UTC()), nil
}

func validateConfigurationInput(id string, input domain.ConfigurationInput) error {
	if !identifier.MatchString(id) || input.ExpectedRevision < 0 || input.Content != nil && (len(*input.Content) < 1 || len(*input.Content) > 1048576) || input.ClearBinding && (input.Enabled || input.Binding != nil) {
		return invalid("proxy configuration is invalid")
	}
	return nil
}

func (s *Management) configurationBinding(ctx context.Context, principal domainidentity.Principal, input domain.ConfigurationInput, existing domain.Instance) (*domain.Binding, error) {
	binding, err := s.authorizeBinding(ctx, principal, input.Binding)
	if err != nil {
		return nil, err
	}
	if input.Enabled && binding == nil && existing.Binding == nil {
		return nil, invalid("an enabled proxy requires a site and host-device binding")
	}
	if binding != nil || input.ClearBinding {
		updated := existing
		updated.Binding = binding
		if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyInstancesUpdate, updated); err != nil {
			return nil, err
		}
	}
	return binding, nil
}

func (s *Management) configurationContent(input domain.ConfigurationInput, existing domain.Instance) (string, string, error) {
	if input.Content == nil {
		if existing.DesiredRevision == 0 {
			return "", "", invalid("content is required for the first proxy configuration")
		}
		return existing.DesiredContentEncrypted, existing.DesiredHash, nil
	}
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, *input.Content)
	if err != nil {
		return "", "", fmt.Errorf("encrypt proxy configuration: %w", err)
	}
	return encrypted, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(*input.Content))), nil
}

func (s *Management) Traffic(ctx context.Context, principal domainidentity.Principal, id string, from, to time.Time) (domain.Traffic, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesView); err != nil {
		return domain.Traffic{}, err
	}
	if !identifier.MatchString(id) {
		return domain.Traffic{}, invalid("proxy instance ID is invalid")
	}
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.Traffic{}, err
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyInstancesView, item); err != nil {
		return domain.Traffic{}, err
	}
	now := s.now().UTC()
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.Add(-time.Hour)
	}
	if !to.After(from) || to.Sub(from) > 24*time.Hour || to.After(now.Add(time.Minute)) {
		return domain.Traffic{}, invalid("proxy traffic window is invalid")
	}
	if missing(s.traffic) {
		return domain.Traffic{}, apperrors.ErrServiceUnavailable
	}
	samples, err := s.traffic.Samples(ctx, id, from, to)
	if err != nil {
		return domain.Traffic{}, err
	}
	calculateTrafficRates(samples)
	return domain.Traffic{InstanceID: id, Supported: slices.Contains(item.Capabilities, "traffic"), Samples: samples}, nil
}

func (s *Management) Connections(ctx context.Context, principal domainidentity.Principal, id string) (domain.ConnectionsSnapshot, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyConnectionsView); err != nil {
		return domain.ConnectionsSnapshot{}, err
	}
	if !identifier.MatchString(id) {
		return domain.ConnectionsSnapshot{}, invalid("proxy instance ID is invalid")
	}
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.ConnectionsSnapshot{}, err
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyConnectionsView, item); err != nil {
		return domain.ConnectionsSnapshot{}, err
	}
	if item.Engine == domain.EngineV2Ray {
		return domain.ConnectionsSnapshot{InstanceID: id, State: "unsupported", Connections: []domain.Connection{}}, nil
	}
	if !slices.Contains(item.Capabilities, "connections") {
		return domain.ConnectionsSnapshot{InstanceID: id, State: "unavailable", Connections: []domain.Connection{}}, nil
	}
	snapshot, err := s.store.Connections(ctx, id)
	if err != nil {
		return domain.ConnectionsSnapshot{}, err
	}
	if snapshot.ProcessEpoch != item.ProcessEpoch || snapshot.ObservedAt != nil && s.now().UTC().Sub(*snapshot.ObservedAt) > 2*time.Minute {
		snapshot.State = "unavailable"
		snapshot.Freshness = "stale"
		return snapshot, nil
	}
	return snapshot, nil
}

func (s *Management) Close(ctx context.Context, principal domainidentity.Principal, id, connectionID string) (domain.CloseCommand, error) {
	return s.CloseWithEpoch(ctx, principal, id, connectionID, "")
}

func (s *Management) CloseWithEpoch(ctx context.Context, principal domainidentity.Principal, id, connectionID, epoch string) (domain.CloseCommand, error) {
	return s.CloseWithInput(ctx, principal, id, connectionID, domain.CloseInput{ProcessEpoch: epoch})
}

func (s *Management) CloseWithInput(ctx context.Context, principal domainidentity.Principal, id, connectionID string, input domain.CloseInput) (domain.CloseCommand, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyConnectionsClose); err != nil {
		return domain.CloseCommand{}, err
	}
	if !identifier.MatchString(id) || !identifier.MatchString(connectionID) {
		return domain.CloseCommand{}, invalid("proxy connection ID is invalid")
	}
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.CloseCommand{}, err
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyConnectionsClose, item); err != nil {
		return domain.CloseCommand{}, err
	}
	if err := validateCloseInput(input); err != nil {
		return domain.CloseCommand{}, err
	}
	store, _ := s.store.(closeRequestStore)
	if input.RequestID != "" {
		if store == nil {
			return domain.CloseCommand{}, apperrors.ErrServiceUnavailable
		}
		now := s.now().UTC()
		if receipt, found, err := store.ReplayClose(ctx, id, connectionID, principal.UserID, input, now); err != nil || found {
			return receipt, err
		}
		if input.IssuedAt.After(now) || !input.ExpiresAt.After(now) {
			return domain.CloseCommand{}, invalid("invalid close command deadline")
		}
	}
	if !slices.Contains(item.Capabilities, "close_connection") {
		return domain.CloseCommand{}, apperrors.ErrConflict
	}
	return s.queueClose(ctx, principal, id, connectionID, input, store)
}

func validateCloseInput(input domain.CloseInput) error {
	if input.RequestID == "" && input.IssuedAt.IsZero() && input.ExpiresAt.IsZero() {
		if input.ProcessEpoch != "" && !identifier.MatchString(input.ProcessEpoch) {
			return invalid("invalid close command epoch")
		}
		return nil
	}
	if !identifier.MatchString(input.RequestID) || !identifier.MatchString(input.ProcessEpoch) || input.IssuedAt.IsZero() ||
		!input.ExpiresAt.After(input.IssuedAt) || input.ExpiresAt.Sub(input.IssuedAt) > 30*time.Second {
		return invalid("invalid close command identity or deadline")
	}
	return nil
}

func (s *Management) queueClose(ctx context.Context, principal domainidentity.Principal, id, connectionID string, input domain.CloseInput, store closeRequestStore) (domain.CloseCommand, error) {
	meta := requestctx.FromContext(ctx)
	if err := s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "NetworkProxyInstance", ResourceName: id,
		Action: "network_access.proxy_connections.close", Result: "requested",
		Summary: "proxy connection close requested", RequestPath: meta.Path,
		RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"instanceId": id, "connectionId": connectionID},
	}); err != nil {
		return domain.CloseCommand{}, fmt.Errorf("audit proxy connection close: %w", err)
	}
	var command domain.CloseCommand
	var err error
	if input.ProcessEpoch != "" {
		if store == nil {
			return command, apperrors.ErrServiceUnavailable
		}
		command, err = store.QueueCloseRequest(ctx, id, connectionID, principal.UserID, input, s.now().UTC())
	} else {
		command, err = s.store.QueueClose(ctx, id, connectionID, principal.UserID, s.now().UTC())
	}
	if err != nil {
		return domain.CloseCommand{}, err
	}
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, "network_access.proxy_connections.close",
		map[string]any{"module": "network_access", "resourceKind": "NetworkProxyInstance", "targetId": id},
		"success", "proxy connection close queued", nil))
	return command, nil
}

func (s *Management) GetClose(ctx context.Context, principal domainidentity.Principal, id, commandID string) (domain.CloseCommand, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyConnectionsView); err != nil {
		return domain.CloseCommand{}, err
	}
	if !identifier.MatchString(id) || !identifier.MatchString(commandID) {
		return domain.CloseCommand{}, invalid("proxy command ID is invalid")
	}
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.CloseCommand{}, err
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessProxyConnectionsView, item); err != nil {
		return domain.CloseCommand{}, err
	}
	return s.store.GetClose(ctx, id, commandID, s.now().UTC())
}

func (s *Management) record(ctx context.Context, principal domainidentity.Principal, action, id string) {
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName,
		Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "NetworkProxyInstance", ResourceName: id,
		Action: action, Result: "success", Summary: action + " succeeded", RequestPath: meta.Path,
		RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"instanceId": id}})
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, action, map[string]any{
		"module": "network_access", "resourceKind": "NetworkProxyInstance", "targetId": id,
	}, "success", action+" succeeded", nil))
}

func invalid(message string) error {
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "proxy_invalid", message, "代理请求无效。")
}

func calculateTrafficRates(samples []domain.TrafficSample) {
	for i := range samples {
		samples[i].RateState = "unknown"
		samples[i].UploadBytesPerSecond = 0
		samples[i].DownloadBytesPerSecond = 0
	}
	for index := 1; index < len(samples); index++ {
		previous, current := samples[index-1], &samples[index]
		seconds := current.ObservedAt.Sub(previous.ObservedAt).Seconds()
		sameEpoch := current.ProcessEpoch != "" && current.ProcessEpoch == previous.ProcessEpoch
		sequenceDelta := current.SampleSequence - previous.SampleSequence
		if seconds > 60 || (sameEpoch && sequenceDelta > 1) {
			current.RateState = "gap"
		}
		if seconds <= 0 || seconds > 60 || !sameEpoch || sequenceDelta != 1 || current.UptimeSeconds <= previous.UptimeSeconds || current.UploadTotal < previous.UploadTotal || current.DownloadTotal < previous.DownloadTotal {
			continue
		}
		current.RateState = "known"
		current.UploadBytesPerSecond = float64(current.UploadTotal-previous.UploadTotal) / seconds
		current.DownloadBytesPerSecond = float64(current.DownloadTotal-previous.DownloadTotal) / seconds
	}
}
