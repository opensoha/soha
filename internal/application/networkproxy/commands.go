package networkproxy

import (
	"context"
	"encoding/json"
	"fmt"
	appaccess "github.com/opensoha/soha/internal/application/access"
	audit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"slices"
	"time"
)

type EpochCloseStore interface {
	CompleteCloseEpoch(context.Context, string, string, string, string, string, time.Time) error
}
type CommandStore interface {
	QueueCommand(context.Context, string, string, domain.RuntimeCommandInput, time.Time) (domain.RuntimeCommandReceipt, error)
	GetCommand(context.Context, string, string, time.Time) (domain.RuntimeCommandReceipt, error)
	ClaimCommand(context.Context, string, string, time.Time) (*domain.RuntimeCommand, error)
	CompleteCommand(context.Context, string, domain.RuntimeCommandResult, time.Time) error
}

// Probe approval is an installation-owned allowlist, never caller-provided URLs.
type ProbeProfiles interface {
	ApprovedProbeProfile(context.Context, string, string) (bool, error)
}

func validateDescriptor(engine string, d *domain.EngineDescriptor) error {
	if d == nil {
		if engine == domain.EngineExternal {
			return invalid("external engine descriptor required")
		}
		return nil
	}
	if !identifier.MatchString(d.EngineID) || !identifier.MatchString(d.AdapterID) || d.Version == "" || len(d.Version) > 64 || d.ProtocolMajor != 1 || d.ProtocolMinor != 0 {
		return invalid("invalid engine descriptor")
	}
	if engine != domain.EngineExternal && d.EngineID != engine {
		return invalid("engine descriptor identity mismatch")
	}
	return validateCapabilities(engine, d.Capabilities)
}

func validateCommand(input domain.RuntimeCommandInput, now time.Time) error {
	if err := validateCommandWindow(input, now); err != nil {
		return err
	}
	if !slices.Contains([]string{"groups", "select", "probe", "refresh", "rules", "dns", "logs", "diagnostics"}, string(input.Kind)) {
		return invalid("unsupported command")
	}
	for _, value := range []string{input.GroupID, input.NodeID, input.SourceID, input.ProbeProfileID} {
		if value != "" && !identifier.MatchString(value) {
			return invalid("invalid command target")
		}
	}
	if input.Kind == "select" && (input.GroupID == "" || input.NodeID == "") || input.Kind == "probe" && input.ProbeProfileID == "" {
		return invalid("missing command target")
	}
	return nil
}
func commandPermission(kind string) string {
	if kind == "select" || kind == "refresh" {
		return appaccess.PermNetworkAccessProxyInstancesUpdate
	}
	return appaccess.PermNetworkAccessProxyInstancesView
}
func (s *Management) QueueCommand(ctx context.Context, p identity.Principal, id string, input domain.RuntimeCommandInput) (domain.RuntimeCommandReceipt, error) {
	var empty domain.RuntimeCommandReceipt
	perm := commandPermission(string(input.Kind))
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, perm); err != nil {
		return empty, err
	}
	if !identifier.MatchString(id) {
		return empty, invalid("invalid instance")
	}

	item, err := s.store.Get(ctx, id)
	if err != nil {
		return empty, err
	}
	if err = s.authorizeScope(ctx, p, perm, item); err != nil {
		return empty, err
	}
	if replay, ok := s.store.(interface {
		ReplayCommand(context.Context, string, string, domain.RuntimeCommandInput, time.Time) (domain.RuntimeCommandReceipt, bool, error)
	}); ok {
		receipt, found, err := replay.ReplayCommand(ctx, id, p.UserID, input, s.now().UTC())
		if err != nil || found {
			return receipt, err
		}
	}
	if err := validateCommand(input, s.now().UTC()); err != nil {
		return empty, err
	}
	if item.ProcessEpoch != input.ProcessEpoch || item.LastSampleAt == nil || s.now().Sub(*item.LastSampleAt) > 2*time.Minute || !slices.Contains(item.Capabilities, string(input.Kind)) {
		return empty, apperrors.ErrConflict
	}
	if input.Kind == "probe" {
		profiles := s.probes
		if missing(profiles) {
			return empty, apperrors.ErrServiceUnavailable
		}
		allowed, err := profiles.ApprovedProbeProfile(ctx, id, input.ProbeProfileID)
		if err != nil {
			return empty, err
		}
		if !allowed {
			return empty, apperrors.ErrAccessDenied
		}
	}
	store, ok := s.store.(CommandStore)
	if !ok {
		return empty, apperrors.ErrServiceUnavailable
	}
	meta := requestctx.FromContext(ctx)
	if err := s.audit.Record(ctx, audit.Entry{ActorID: p.UserID, ActorName: p.UserName, Roles: p.Roles, Teams: p.Teams, ResourceKind: "NetworkProxyInstance", ResourceName: id, Action: "network_access.proxy_commands." + string(input.Kind), Result: "requested", Summary: "typed proxy command requested", RequestID: meta.RequestID, RequestPath: meta.Path, RequestMethod: meta.Method, SourceIP: meta.SourceIP, Metadata: map[string]any{"instanceId": id, "requestId": input.RequestID, "kind": input.Kind}}); err != nil {
		return empty, fmt.Errorf("audit typed proxy command: %w", err)
	}
	receipt, err := store.QueueCommand(ctx, id, p.UserID, input, s.now().UTC())
	if err == nil {
		s.record(ctx, p, "network_access.proxy_commands."+string(input.Kind), id)
	}
	return receipt, err
}
func (s *Management) GetCommand(ctx context.Context, p identity.Principal, id, commandID string) (domain.RuntimeCommandReceipt, error) {
	var empty domain.RuntimeCommandReceipt
	if _, err := s.Get(ctx, p, id); err != nil {
		return empty, err
	}
	if !identifier.MatchString(commandID) {
		return empty, invalid("invalid command")
	}
	store, ok := s.store.(CommandStore)
	if !ok {
		return empty, apperrors.ErrServiceUnavailable
	}
	return store.GetCommand(ctx, id, commandID, s.now().UTC())
}
func (s *Runtime) NextCommand(ctx context.Context, identity networkidentity.Identity, epoch string) (*domain.RuntimeCommand, error) {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return nil, err
	}
	if !identifier.MatchString(epoch) || item.ProcessEpoch != epoch {
		return nil, apperrors.ErrConflict
	}
	store, ok := s.store.(CommandStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	return store.ClaimCommand(ctx, item.ID, epoch, s.now().UTC())
}
func (s *Runtime) CompleteCommand(ctx context.Context, identity networkidentity.Identity, result domain.RuntimeCommandResult) error {
	item, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	if !identifier.MatchString(result.CommandID) || !identifier.MatchString(result.RequestID) || item.ProcessEpoch != result.ProcessEpoch || !slices.Contains([]string{"succeeded", "failed", "unsupported", "cancelled", "unavailable", "unknown"}, result.Status) || len(result.ReasonCode) > 128 {
		return invalid("invalid command result")
	}
	raw, err := json.Marshal(result.Result)
	if err != nil || len(raw) > 262144 {
		return invalid("command result exceeds limit")
	}
	store, ok := s.store.(CommandStore)
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	return store.CompleteCommand(ctx, item.ID, result, s.now().UTC())
}

func (s *Management) SetProbeProfiles(profiles ProbeProfiles) { s.probes = profiles }

func (s *Management) CancelCommand(ctx context.Context, p identity.Principal, id, commandID string) error {
	if err := s.AuthorizePublication(ctx, p, id, "update"); err != nil {
		return err
	}
	if !identifier.MatchString(commandID) {
		return invalid("invalid command")
	}
	store, ok := s.store.(interface {
		CancelCommand(context.Context, string, string, time.Time) error
	})
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	if err := store.CancelCommand(ctx, id, commandID, s.now().UTC()); err != nil {
		return err
	}
	s.record(ctx, p, "network_access.proxy_commands.cancel", id)
	return nil
}

func validateCommandWindow(input domain.RuntimeCommandInput, now time.Time) error {
	if !identifier.MatchString(input.RequestID) || !identifier.MatchString(input.ProcessEpoch) || input.IssuedAt.Before(now.Add(-30*time.Second)) || input.IssuedAt.After(now) || !input.ExpiresAt.After(now) || input.ExpiresAt.After(input.IssuedAt.Add(30*time.Second)) || input.Limit < 0 || input.Limit > 200 || len(input.Cursor) > 512 {
		return invalid("invalid command deadline or identity")
	}
	return nil
}
