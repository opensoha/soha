package networkproxy

import (
	"context"
	"crypto/sha256"
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
	List(context.Context, string, string, int) ([]domain.Instance, error)
	UpdateConfiguration(context.Context, string, int64, bool, string, string, time.Time) (domain.Instance, error)
	Connections(context.Context, string) (domain.ConnectionsSnapshot, error)
	QueueClose(context.Context, string, string, string, time.Time) (domain.CloseCommand, error)
}

type TrafficReader interface {
	Samples(context.Context, string, time.Time, time.Time) ([]domain.TrafficSample, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}
type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Management struct {
	store       ManagementStore
	traffic     TrafficReader
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	keys        keyring.Ring
	now         func() time.Time
}

func NewManagement(store ManagementStore, traffic TrafficReader, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder, keys keyring.Ring) (*Management, error) {
	for name, value := range map[string]any{"store": store, "permissions": permissions, "audit": audit, "operations": operations} {
		if missing(value) {
			return nil, fmt.Errorf("proxy management: %s is required", name)
		}
	}
	if keys.Active().ID() == "" {
		return nil, fmt.Errorf("proxy management: encryption key is required")
	}
	return &Management{store: store, traffic: traffic, permissions: permissions, audit: audit, operations: operations, keys: keys, now: time.Now}, nil
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
	return engine == domain.EngineMihomo || engine == domain.EngineSingBox || engine == domain.EngineV2Ray
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
	case item.LastSampleAt != nil && now.Sub(*item.LastSampleAt) <= 2*time.Minute && item.Health == "healthy":
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
	if !identifier.MatchString(input.ID) || input.Name == "" || len(input.Name) > 128 || len(input.Host) > 128 || !validEngine(input.Engine) {
		return domain.Instance{}, invalid("proxy instance input is invalid")
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
	items, err := s.store.List(ctx, search, engine, limit)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	for index := range items {
		items[index] = public(items[index], now)
	}
	return items, nil
}

func (s *Management) Get(ctx context.Context, principal domainidentity.Principal, id string) (domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesView); err != nil {
		return domain.Instance{}, err
	}
	if !identifier.MatchString(id) {
		return domain.Instance{}, invalid("proxy instance ID is invalid")
	}
	item, err := s.store.Get(ctx, id)
	return public(item, s.now().UTC()), err
}

func (s *Management) UpdateConfiguration(ctx context.Context, principal domainidentity.Principal, id string, input domain.ConfigurationInput) (domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessProxyInstancesUpdate); err != nil {
		return domain.Instance{}, err
	}
	if !identifier.MatchString(id) || input.ExpectedRevision < 0 || len(input.Content) < 1 || len(input.Content) > 1048576 {
		return domain.Instance{}, invalid("proxy configuration is invalid")
	}
	if _, err := s.store.Get(ctx, id); err != nil {
		return domain.Instance{}, err
	}
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, input.Content)
	if err != nil {
		return domain.Instance{}, fmt.Errorf("encrypt proxy configuration: %w", err)
	}
	digest := sha256.Sum256([]byte(input.Content))
	item, err := s.store.UpdateConfiguration(ctx, id, input.ExpectedRevision, input.Enabled, encrypted, fmt.Sprintf("sha256:%x", digest), s.now().UTC())
	if err != nil {
		return domain.Instance{}, err
	}
	s.record(ctx, principal, "network_access.proxy_instances.update", id)
	return public(item, s.now().UTC()), nil
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
	for index := 1; index < len(samples); index++ {
		previous, current := samples[index-1], &samples[index]
		seconds := current.ObservedAt.Sub(previous.ObservedAt).Seconds()
		if seconds <= 0 || current.UptimeSeconds <= previous.UptimeSeconds || current.UploadTotal < previous.UploadTotal || current.DownloadTotal < previous.DownloadTotal {
			continue
		}
		current.UploadBytesPerSecond = float64(current.UploadTotal-previous.UploadTotal) / seconds
		current.DownloadBytesPerSecond = float64(current.DownloadTotal-previous.DownloadTotal) / seconds
	}
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
	if snapshot.ObservedAt != nil && s.now().UTC().Sub(*snapshot.ObservedAt) > time.Minute {
		return domain.ConnectionsSnapshot{InstanceID: id, State: "unavailable", Connections: []domain.Connection{}}, nil
	}
	return snapshot, nil
}

func (s *Management) Close(ctx context.Context, principal domainidentity.Principal, id, connectionID string) (domain.CloseCommand, error) {
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
	if !slices.Contains(item.Capabilities, "close_connection") {
		return domain.CloseCommand{}, apperrors.ErrConflict
	}
	snapshot, err := s.store.Connections(ctx, id)
	if err != nil {
		return domain.CloseCommand{}, err
	}
	if snapshot.ObservedAt == nil || s.now().UTC().Sub(*snapshot.ObservedAt) > time.Minute {
		return domain.CloseCommand{}, apperrors.ErrConflict
	}
	found := false
	for _, connection := range snapshot.Connections {
		if connection.ID == connectionID {
			found = true
			break
		}
	}
	if !found {
		return domain.CloseCommand{}, apperrors.ErrNotFound
	}
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
	command, err := s.store.QueueClose(ctx, id, connectionID, principal.UserID, s.now().UTC())
	if err != nil {
		return domain.CloseCommand{}, err
	}
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, "network_access.proxy_connections.close",
		map[string]any{"module": "network_access", "resourceKind": "NetworkProxyInstance", "targetId": id},
		"success", "proxy connection close queued", nil))
	return command, nil
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
