package assistantmesh

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DelegationStore interface {
	WithReceptionTransaction(context.Context, func(context.Context) error) error
	PutManagedDevice(context.Context, domain.ManagedDevice) error
	GetManagedDevice(context.Context, string) (domain.ManagedDevice, error)
	CreateDelegation(context.Context, domain.DelegationRecord) (domain.DelegationRecord, error)
	GetDelegation(context.Context, string) (domain.DelegationRecord, error)
	ListDelegations(context.Context, string, bool) ([]domain.DelegationRecord, error)
	UpdateDelegation(context.Context, string, string, func(context.Context, *domain.DelegationRecord) error) (domain.DelegationRecord, error)
}

type Delegations struct {
	reception *Reception
	store     DelegationStore
}

func NewDelegations(reception *Reception, store DelegationStore) *Delegations {
	return &Delegations{reception: reception, store: store}
}

type DelegationInput struct {
	OwnerID    string `json:"ownerId"`
	RequestID  string `json:"requestId"`
	Capability string `json:"capability"`
	Field      string `json:"field"`
}

type DelegationView struct {
	domain.Delegation
	CallerName string `json:"callerName"`
	OwnerName  string `json:"ownerName"`
}

func (s *Delegations) Register(ctx context.Context, p identity.Principal, session, deviceID, proofHash string) error {
	if _, err := s.reception.prepare(ctx, p); err != nil {
		return err
	}
	if session == "" || !validReceptionID(deviceID) || !validDelegationHash(proofHash) {
		return apperrors.ErrInvalidArgument
	}
	return s.store.PutManagedDevice(ctx, domain.ManagedDevice{
		OwnerID: p.UserID, DeviceID: deviceID, SessionID: session, ProofHash: proofHash,
	})
}

func (s *Delegations) Create(ctx context.Context, p identity.Principal, input DelegationInput) (DelegationView, error) {
	p, err := s.reception.prepare(ctx, p)
	if err != nil {
		return DelegationView{}, err
	}
	if input.Capability != domain.ManagedOSVersion || !validDelegationField(input.Field) ||
		!validReceptionID(input.RequestID) || !validReceptionID(input.OwnerID) || input.OwnerID == p.UserID {
		return DelegationView{}, apperrors.ErrInvalidArgument
	}
	owner, err := s.visibleOwner(ctx, p, input.OwnerID)
	if err != nil {
		return DelegationView{}, err
	}
	if _, err = s.store.GetManagedDevice(ctx, owner.UserID); err != nil {
		return DelegationView{}, err
	}
	settings, err := s.reception.assets.config.ResolveAssistantMeshSettings(ctx)
	if err != nil {
		return DelegationView{}, err
	}
	now := nowUTC()
	id := uuid.NewString()
	var item domain.DelegationRecord
	err = s.store.WithReceptionTransaction(ctx, func(ctx context.Context) error {
		var err error
		item, err = s.store.CreateDelegation(ctx, domain.DelegationRecord{
			RequestID: input.RequestID,
			Delegation: domain.Delegation{
				ID: id, CallerID: p.UserID, OwnerID: owner.UserID, Field: input.Field,
				Capability: input.Capability, InputHash: delegationDigest(input.Capability + "\n" + input.Field),
				Status: domain.DelegationAwaitingApproval, CreatedAt: now, ExpiresAt: now.Add(time.Duration(settings.DelegationLifetimeHours) * time.Hour),
			},
		})
		if err != nil || item.ID != id {
			return err
		}
		return s.audit(ctx, p, "request", item)
	})
	if err != nil {
		return DelegationView{}, err
	}
	return delegationView(item, p.UserName, owner.UserName), nil
}

func (s *Delegations) visibleOwner(ctx context.Context, caller identity.Principal, ownerID string) (identity.Principal, error) {
	owner, err := s.reception.openOwner(ctx, ownerID)
	if err != nil {
		return identity.Principal{}, err
	}
	facts, err := s.reception.facts(ctx, caller, ownerID)
	if err == nil && len(facts) == 0 {
		err = apperrors.ErrNotFound
	}
	return owner, err
}

func (s *Delegations) view(ctx context.Context, item domain.DelegationRecord, actor identity.Principal) (DelegationView, error) {
	if item.CallerID != actor.UserID && item.OwnerID != actor.UserID {
		return DelegationView{}, apperrors.ErrNotFound
	}
	if item.Status == domain.DelegationInvalidated {
		return delegationView(item, "", ""), nil
	}
	caller, err := s.reception.identities.CurrentPrincipal(ctx, item.CallerID)
	if item.CallerID == actor.UserID {
		caller, err = actor, nil
	}
	if err == nil {
		err = s.reception.requireHeya(ctx, caller)
	}
	owner := identity.Principal{}
	if err == nil {
		owner, err = s.visibleOwner(ctx, caller, item.OwnerID)
	}
	if errors.Is(err, apperrors.ErrUnauthorized) || errors.Is(err, apperrors.ErrAccessDenied) {
		err = apperrors.ErrNotFound
	}
	if err != nil {
		return DelegationView{}, err
	}
	return delegationView(item, caller.UserName, owner.UserName), nil
}

func (s *Delegations) Get(ctx context.Context, p identity.Principal, id string) (DelegationView, error) {
	p, err := s.reception.prepare(ctx, p)
	if err != nil {
		return DelegationView{}, err
	}
	if !validReceptionID(id) {
		return DelegationView{}, apperrors.ErrNotFound
	}
	item, err := s.store.GetDelegation(ctx, id)
	if err != nil {
		return DelegationView{}, err
	}
	return s.view(ctx, item, p)
}

func (s *Delegations) List(ctx context.Context, p identity.Principal, direction string) ([]DelegationView, error) {
	p, err := s.reception.prepare(ctx, p)
	if err != nil {
		return nil, err
	}
	if direction != "inbox" && direction != "sent" {
		return nil, apperrors.ErrInvalidArgument
	}
	items, err := s.store.ListDelegations(ctx, p.UserID, direction == "inbox")
	if err != nil {
		return nil, err
	}
	views := make([]DelegationView, 0, len(items))
	for _, item := range items {
		view, err := s.view(ctx, item, p)
		if errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func delegationView(item domain.DelegationRecord, caller, owner string) DelegationView {
	if item.Status != domain.DelegationCompleted {
		item.Result = ""
	}
	if item.ClosedAt == nil && !nowUTC().Before(item.ExpiresAt) {
		item.Status = domain.DelegationExpired
	}
	if item.ClosedAt != nil && nowUTC().Sub(*item.ClosedAt) >= retention {
		item.Result = ""
	}
	return DelegationView{Delegation: item.Delegation, CallerName: caller, OwnerName: owner}
}

func (s *Delegations) audit(ctx context.Context, p identity.Principal, action string, item domain.DelegationRecord) error {
	metadata := map[string]any{
		"callerId": item.CallerID, "ownerId": item.OwnerID, "capability": item.Capability,
		"field": item.Field, "inputHash": item.InputHash, "mode": item.Mode, "status": item.Status,
		"source": "heya",
	}
	if item.Status == domain.DelegationCompleted {
		metadata["resultHash"] = item.ResultHash
	}
	return s.reception.assets.audit(ctx, p, "delegation."+action, item.ID, "success", metadata)
}

func delegationDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validDelegationHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func validDelegationField(field string) bool {
	return field == "productVersion" || field == "buildVersion"
}

var osProductVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}$`)
var osBuildVersion = regexp.MustCompile(`^[0-9]+[A-Z][0-9]+[a-z]?$`)

func validDelegationResult(field, result string) bool {
	if len(result) > 128 {
		return false
	}
	switch field {
	case "productVersion":
		return osProductVersion.MatchString(result)
	case "buildVersion":
		return osBuildVersion.MatchString(result)
	default:
		return false
	}
}

func newDelegationTicket() (string, error) {
	var token [32]byte
	if _, err := cryptorand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func equalDelegationHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
