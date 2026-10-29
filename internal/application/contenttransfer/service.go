package contenttransfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	access "github.com/opensoha/soha/internal/domain/access"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const Prefix = "network_access."
const View = Prefix + "content_transfers.view"
const Create = Prefix + "content_transfers.create"
const Download = Prefix + "content_transfers.download"
const Delete = Prefix + "content_transfers.delete"
const DestinationView = Prefix + "content_destinations.view"
const DestinationUpdate = Prefix + "content_destinations.update"
const DestinationPublish = Prefix + "content_destinations.publish"
const Events = Prefix + "content_transfer_events.view"
const Review = Prefix + "content_transfer_appeals.review"

type Store interface {
	Destinations(context.Context) ([]d.Destination, error)
	Destination(context.Context, string) (d.Destination, error)
	Save(context.Context, string, string, d.SaveInput) (d.Destination, error)
	Publish(context.Context, string, string, int64) (d.Destination, error)
	Record(context.Context, string) (d.Record, error)
	Records(context.Context, string, string, string, int, int) (d.Page, error)
	Create(context.Context, d.Record) error
	Update(context.Context, d.Record, int64, string, string) error
	Consume(context.Context, string, string, string) error
	WithCurrent(context.Context, []d.Gate, func(context.Context) error) error
}
type Backend interface {
	Identity() string
	SnapshotIdentity() string
	Open(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}
type Backends interface {
	Validate(context.Context, identity.Principal, d.Destination) error
	Prepare(context.Context, identity.Principal, d.Destination) (Backend, error)
}
type Scope interface {
	Authorize(context.Context, access.Request) (access.Decision, error)
}
type Service struct {
	stepUp      appdlp.StepUpVerifier
	store       Store
	backends    Backends
	scope       Scope
	permissions *appaccess.PermissionResolver
	slots       chan struct{}
	linkage     NetworkLinkage
}

func New(store Store, backends Backends, scope Scope, permissions *appaccess.PermissionResolver, opts ...Option) (*Service, error) {
	for _, v := range []any{store, backends, scope, permissions} {
		if v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil() {
			return nil, fmt.Errorf("managed content transfer dependencies are required")
		}
	}
	s := &Service{store: store, backends: backends, scope: scope, permissions: permissions, slots: make(chan struct{}, 8)}
	for _, opt := range opts {
		opt(s)
	}
	if s.linkage != nil {
		v := reflect.ValueOf(s.linkage)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return nil, fmt.Errorf("content network linkage must not be typed nil")
		}
	}
	return s, nil
}
func (s *Service) authorize(ctx context.Context, p identity.Principal, key string, dest d.Destination) error {
	if p.UserID == "" {
		return apperrors.ErrAccessDenied
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return err
	}
	decision, err := s.scope.Authorize(ctx, access.Request{Principal: p, PermissionKey: key, Subject: access.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags}, Resource: access.ResourceAttributes{Group: "network_access", Kind: "ContentTransferDestination", Name: dest.ID, Owner: dest.OwnerID, Labels: map[string]string{"content.destinationId": dest.ID}}, Context: access.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()}})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func (s *Service) global(ctx context.Context, p identity.Principal, key string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return err
	}
	decision, err := s.scope.Authorize(ctx, access.Request{Principal: p, PermissionKey: key, Subject: access.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags}, Resource: access.ResourceAttributes{Group: "network_access", Kind: "NetworkSecurityScope", Name: "all-subjects"}, Context: access.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()}})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func (s *Service) destination(ctx context.Context, p identity.Principal, id, key string) (d.Destination, error) {
	if !validID(id) {
		return d.Destination{}, apperrors.ErrInvalidArgument
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return d.Destination{}, err
	}
	dest, err := s.store.Destination(ctx, id)
	if err != nil {
		return dest, err
	}
	return dest, s.authorize(ctx, p, key, dest)
}
func (s *Service) Destinations(ctx context.Context, p identity.Principal, admin bool) ([]d.Destination, error) {
	key := View
	if admin {
		key = DestinationView
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return nil, err
	}
	return s.destinationsFor(ctx, p, key, !admin)
}
func (s *Service) ReviewDestinations(ctx context.Context, p identity.Principal) ([]d.Destination, error) {
	if err := s.global(ctx, p, Events); err != nil {
		return nil, err
	}
	return s.destinationsFor(ctx, p, Events, false)
}
func (s *Service) destinationsFor(ctx context.Context, p identity.Principal, key string, enabledOnly bool) ([]d.Destination, error) {
	items, err := s.store.Destinations(ctx)
	if err != nil {
		return nil, err
	}
	result := []d.Destination{}
	for _, dest := range items {
		if enabledOnly && (dest.Published == nil || !dest.Published.Enabled) {
			continue
		}
		if err := s.authorize(ctx, p, key, dest); err != nil {
			if errors.Is(err, apperrors.ErrAccessDenied) {
				continue
			}
			return nil, err
		}
		result = append(result, dest)
	}
	return result, nil
}
func (s *Service) GetDestination(ctx context.Context, p identity.Principal, id string) (d.Destination, error) {
	return s.destination(ctx, p, id, DestinationView)
}
func (s *Service) SaveDestination(ctx context.Context, p identity.Principal, id string, in d.SaveInput) (d.Destination, error) {
	if !validID(id) || in.ExpectedRevision < 0 {
		return d.Destination{}, apperrors.ErrInvalidArgument
	}
	if err := ValidateConfig(in.Draft); err != nil {
		return d.Destination{}, err
	}
	if err := s.authorizeNetworkPolicy(ctx, p, in.Draft.Policy); err != nil {
		return d.Destination{}, err
	}
	if err := s.global(ctx, p, DestinationUpdate); err != nil {
		return d.Destination{}, err
	}
	dest, err := s.store.Destination(ctx, id)
	if errors.Is(err, apperrors.ErrNotFound) {
		if in.ExpectedRevision != 0 {
			return dest, apperrors.ErrConflict
		}
		dest = d.Destination{ID: id, OwnerID: p.UserID}
	} else if err != nil {
		return dest, err
	}
	if err := s.authorize(ctx, p, DestinationUpdate, dest); err != nil {
		return dest, err
	}
	return s.store.Save(ctx, id, p.UserID, in)
}
func (s *Service) PublishDestination(ctx context.Context, p identity.Principal, id string, in d.PublishInput) (d.Destination, error) {
	if in.ExpectedRevision < 1 {
		return d.Destination{}, apperrors.ErrInvalidArgument
	}
	if err := s.global(ctx, p, DestinationPublish); err != nil {
		return d.Destination{}, err
	}
	dest, err := s.destination(ctx, p, id, DestinationPublish)
	if err != nil {
		return dest, err
	}
	if dest.Revision != in.ExpectedRevision {
		return dest, apperrors.ErrConflict
	}
	if err := s.authorizeNetworkPolicy(ctx, p, dest.Draft.Policy); err != nil {
		return d.Destination{}, err
	}
	if err := ValidateConfig(dest.Draft); err != nil {
		return dest, err
	}
	// Resolve a frozen backend/credential scope without connecting to or modifying
	// the destination. Disabled publication remains available for urgent closure.
	if dest.Draft.Enabled {
		candidate := dest
		candidate.Published = &candidate.Draft
		if err := s.backends.Validate(ctx, p, candidate); err != nil {
			return dest, err
		}
		if _, err := s.backends.Prepare(ctx, p, candidate); err != nil {
			return dest, err
		}
	}
	return s.store.Publish(ctx, id, p.UserID, in.ExpectedRevision)
}
func currentRecord(r d.Record) d.Record {
	if (r.State == "prepared" || r.State == "sending") && !r.Private.Deadline.After(time.Now().UTC()) {
		r.State = "unknown"
		r.ErrorCode = "effect_unconfirmed"
	}
	return r
}
func (s *Service) OwnRecord(ctx context.Context, p identity.Principal, id string) (d.Record, error) {
	if !validID(id) {
		return d.Record{}, apperrors.ErrInvalidArgument
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, View); err != nil {
		return d.Record{}, err
	}
	r, err := s.store.Record(ctx, id)
	if err != nil {
		return r, err
	}
	if r.ActorID != p.UserID {
		return d.Record{}, apperrors.ErrAccessDenied
	}
	if _, err := s.destination(ctx, p, r.DestinationID, View); err != nil {
		return d.Record{}, err
	}
	if err := s.recordScope(ctx, p, r, View); err != nil {
		return d.Record{}, err
	}
	return currentRecord(r), nil
}
func (s *Service) Records(ctx context.Context, p identity.Principal, kind, dest string, offset, limit int) (d.Page, error) {
	if offset < 0 || offset > 10000 || limit < 1 || limit > 100 {
		return d.Page{}, apperrors.ErrInvalidArgument
	}
	if kind == "cases" {
		if _, err := s.destination(ctx, p, dest, Events); err != nil {
			return d.Page{}, err
		}
		if err := s.global(ctx, p, Events); err != nil {
			return d.Page{}, err
		}
	} else {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, View); err != nil {
			return d.Page{}, err
		}
	}
	result, err := s.store.Records(ctx, p.UserID, dest, kind, offset, limit)
	if err != nil {
		return result, err
	}
	// A page is always owned by its caller, or by the explicitly authorized case
	// destination. Rechecking every referenced scope fails rather than leaking it.
	key := View
	if kind == "cases" {
		key = Events
	}
	seen := map[string]bool{}
	for i, r := range result.Items {
		ids := []string{r.DestinationID}
		for _, g := range r.Gates {
			ids = append(ids, g.DestinationID)
		}
		for _, id := range ids {
			if !seen[id] {
				if _, err := s.destination(ctx, p, id, key); err != nil {
					return d.Page{}, err
				}
				seen[id] = true
			}
		}
		result.Items[i] = currentRecord(r)
	}

	return result, nil
}
