package endpointcontrol

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"reflect"
	"time"

	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	appaccess "github.com/opensoha/soha/internal/application/access"
	access "github.com/opensoha/soha/internal/domain/access"
	"github.com/opensoha/soha/internal/domain/identity"
	network "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const Prefix = "network_access.endpoint_controls."

type Store interface {
	View(context.Context, string) (ec.View, error)
	Save(context.Context, string, string, ec.SaveInput, time.Time) (ec.Policy, error)
	Publish(context.Context, string, string, int64, time.Time) (ec.Policy, error)
	Desired(context.Context, runtime.Credential, string) (ec.Desired, error)
	Report(context.Context, runtime.Credential, ec.Receipt) error
}
type Devices interface {
	GetDevice(context.Context, string) (network.Device, error)
}
type Scope interface {
	Authorize(context.Context, access.Request) (access.Decision, error)
}
type Runtime struct{ store Store }

func NewRuntime(store Store) (*Runtime, error) {
	if store == nil {
		return nil, fmt.Errorf("endpoint channel store required")
	}
	return &Runtime{store: store}, nil
}

type Service struct {
	*Runtime
	store       Store
	devices     Devices
	scope       Scope
	permissions *appaccess.PermissionResolver
}

func New(store Store, devices Devices, scope Scope, permissions *appaccess.PermissionResolver) (*Service, error) {
	for _, v := range []any{store, devices, scope, permissions} {
		if v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil() {
			return nil, fmt.Errorf("endpoint control dependencies required")
		}
	}
	return &Service{Runtime: &Runtime{store: store}, store: store, devices: devices, scope: scope, permissions: permissions}, nil
}
func (s *Service) authorize(ctx context.Context, p identity.Principal, id, action string) (network.Device, error) {
	key := Prefix + action
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return network.Device{}, err
	}
	d, err := s.devices.GetDevice(ctx, id)
	if err != nil {
		return d, err
	}
	resource := access.ResourceAttributes{Group: "network_access", Kind: "EndpointDevice", Name: d.ID, Owner: d.OwnerUserID, Labels: map[string]string{"network.siteId": d.SiteID, "network.deviceId": d.ID, "network.ownerUserId": d.OwnerUserID, "network.spaceId": ""}}
	if err := s.checkScope(ctx, p, key, resource); err != nil {
		return d, err
	}
	if action != "view" {
		err = s.checkScope(ctx, p, key, access.ResourceAttributes{Group: "network_access", Kind: "NetworkSecurityScope", Name: "all-subjects"})
		if err != nil {
			return d, err
		}
		if d.Platform != "windows" || d.Status != "active" {
			return d, apperrors.ErrInvalidArgument
		}
	}
	return d, nil
}
func (s *Service) checkScope(ctx context.Context, p identity.Principal, key string, r access.ResourceAttributes) error {
	d, err := s.scope.Authorize(ctx, access.Request{Principal: p, PermissionKey: key, Subject: access.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags}, Resource: r, Context: access.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()}})
	if err != nil {
		return err
	}
	if !d.Allowed {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (ec.View, error) {
	d, err := s.authorize(ctx, p, id, "view")
	if err != nil {
		return ec.View{}, err
	}
	return s.store.View(ctx, d.ID)
}
func (s *Service) Save(ctx context.Context, p identity.Principal, id string, input ec.SaveInput) (ec.Policy, error) {
	d, err := s.authorize(ctx, p, id, "update")
	if err != nil {
		return ec.Policy{}, err
	}
	if input.ExpectedRevision < 0 {
		return ec.Policy{}, apperrors.ErrInvalidArgument
	}
	return s.store.Save(ctx, d.ID, p.UserID, input, d.UpdatedAt)
}
func (s *Service) Publish(ctx context.Context, p identity.Principal, id string, input ec.PublishInput) (ec.Policy, error) {
	d, err := s.authorize(ctx, p, id, "publish")
	if err != nil {
		return ec.Policy{}, err
	}
	if input.ExpectedRevision < 1 {
		return ec.Policy{}, apperrors.ErrInvalidArgument
	}
	return s.store.Publish(ctx, d.ID, p.UserID, input.ExpectedRevision, d.UpdatedAt)
}
func (s *Runtime) Desired(ctx context.Context, c runtime.Credential) (ec.Desired, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ec.Desired{}, err
	}
	return s.store.Desired(ctx, c, base64.RawURLEncoding.EncodeToString(nonce[:]))
}
func (s *Runtime) Report(ctx context.Context, c runtime.Credential, raw []byte) error {
	var in ec.Receipt
	if ec.Decode(raw, &in) != nil || !ec.ValidReceipt(in, time.Now().UTC()) || in.RuntimeID != c.RuntimeID || in.CredentialID != c.ID || in.DeviceID != c.DeviceID {
		return apperrors.ErrInvalidArgument
	}
	return s.store.Report(ctx, c, in)
}
