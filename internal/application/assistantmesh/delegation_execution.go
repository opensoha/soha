package assistantmesh

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NativeDelegationInput struct {
	DeviceID   string `json:"deviceId"`
	Proof      string `json:"proof"`
	InputHash  string `json:"inputHash"`
	Mode       string `json:"mode,omitempty"`
	Ticket     string `json:"ticket,omitempty"`
	Fence      int    `json:"fence,omitempty"`
	ResultHash string `json:"resultHash,omitempty"`
	Nonce      string `json:"nonce,omitempty"`
	Result     string `json:"result,omitempty"`
	Failed     bool   `json:"failed,omitempty"`
}

type DelegationApproval struct {
	Delegation DelegationView `json:"delegation"`
	Ticket     string         `json:"ticket"`
	Fence      int            `json:"fence"`
}

func (s *Delegations) Approve(ctx context.Context, p identity.Principal, id, session string, input NativeDelegationInput) (DelegationApproval, error) {
	if (input.Mode != "preview" && input.Mode != "direct") || input.Fence != 0 || input.Ticket != "" ||
		input.ResultHash != "" || input.Nonce != "" || input.Result != "" || input.Failed {
		return DelegationApproval{}, apperrors.ErrInvalidArgument
	}
	ticket, err := newDelegationTicket()
	if err != nil {
		return DelegationApproval{}, err
	}
	fence := 0
	view, err := s.nativeTransition(ctx, p, id, session, "approve", input, func(ctx context.Context, item *domain.DelegationRecord, now time.Time) error {
		if item.Status != domain.DelegationAwaitingApproval &&
			(item.Status != domain.DelegationApproved || item.TicketExpiresAt.After(now)) {
			return apperrors.ErrConflict
		}
		binding, err := s.ticketDigest(ctx, item, ticket)
		if err != nil {
			return err
		}
		item.Status = domain.DelegationApproved
		item.Mode, item.DeviceID = input.Mode, input.DeviceID
		item.Fence++
		fence = item.Fence
		item.TicketHash = binding
		item.TicketExpiresAt = now.Add(time.Minute)
		if item.ExpiresAt.Before(item.TicketExpiresAt) {
			item.TicketExpiresAt = item.ExpiresAt
		}
		return nil
	})
	return DelegationApproval{Delegation: view, Ticket: ticket, Fence: fence}, err
}

func (s *Delegations) Claim(ctx context.Context, p identity.Principal, id, session string, input NativeDelegationInput) (DelegationView, error) {
	if !validDelegationHash(input.Ticket) || input.Mode != "" || input.ResultHash != "" || input.Nonce != "" || input.Result != "" || input.Failed {
		return DelegationView{}, apperrors.ErrInvalidArgument
	}
	return s.nativeTransition(ctx, p, id, session, "claim", input, func(ctx context.Context, item *domain.DelegationRecord, now time.Time) error {
		if item.Status != domain.DelegationApproved || !now.Before(item.TicketExpiresAt) {
			return apperrors.ErrConflict
		}
		binding, err := s.ticketDigest(ctx, item, input.Ticket)
		if err != nil {
			return err
		}
		if !equalDelegationHash(item.TicketHash, binding) {
			return apperrors.ErrConflict
		}
		item.Status = domain.DelegationExecuting
		item.TicketHash, item.TicketExpiresAt = "", time.Time{}
		return nil
	})
}

func (s *Delegations) Report(ctx context.Context, p identity.Principal, id, session string, input NativeDelegationInput) (DelegationView, error) {
	if input.Mode != "" || input.Ticket != "" || input.Nonce != "" {
		return DelegationView{}, apperrors.ErrInvalidArgument
	}
	return s.nativeTransition(ctx, p, id, session, "report", input, func(_ context.Context, item *domain.DelegationRecord, now time.Time) error {
		if item.Status != domain.DelegationExecuting {
			return apperrors.ErrConflict
		}
		if input.Failed {
			if input.Result != "" || input.ResultHash != "" {
				return apperrors.ErrInvalidArgument
			}
			item.Status, item.ClosedAt = domain.DelegationFailed, &now
		} else if item.Mode == "preview" && input.Result == "" && validDelegationHash(input.ResultHash) {
			item.Status, item.ResultHash = domain.DelegationPreviewed, input.ResultHash
		} else if item.Mode == "direct" && validDelegationResult(item.Field, input.Result) &&
			equalDelegationHash(input.ResultHash, delegationDigest(input.Result)) {
			item.Status, item.ResultHash = domain.DelegationCompleted, input.ResultHash
			item.Result, item.ClosedAt = input.Result, &now
		} else {
			return apperrors.ErrInvalidArgument
		}
		return nil
	})
}

func (s *Delegations) Deliver(ctx context.Context, p identity.Principal, id, session string, input NativeDelegationInput) (DelegationView, error) {
	if input.Mode != "" || input.Ticket != "" || input.ResultHash != "" || !validDelegationHash(input.Nonce) || input.Failed {
		return DelegationView{}, apperrors.ErrInvalidArgument
	}
	return s.nativeTransition(ctx, p, id, session, "deliver", input, func(_ context.Context, item *domain.DelegationRecord, now time.Time) error {
		if item.Status != domain.DelegationPreviewed {
			return apperrors.ErrConflict
		}
		if !validDelegationResult(item.Field, input.Result) || !equalDelegationHash(item.ResultHash, delegationDigest(input.Nonce+"\n"+input.Result)) {
			return apperrors.ErrInvalidArgument
		}
		item.Status, item.Result, item.ClosedAt = domain.DelegationCompleted, input.Result, &now
		return nil
	})
}

func (s *Delegations) Decline(ctx context.Context, p identity.Principal, id string) (DelegationView, error) {
	return s.change(ctx, p, id, "decline", func(_ context.Context, item *domain.DelegationRecord, now time.Time) error {
		switch item.Status {
		case domain.DelegationAwaitingApproval, domain.DelegationApproved, domain.DelegationPreviewed:
			item.Status, item.ClosedAt = domain.DelegationDeclined, &now
			item.TicketHash, item.TicketExpiresAt, item.ResultHash = "", time.Time{}, ""
			return nil
		default:
			return apperrors.ErrConflict
		}
	})
}

func (s *Delegations) nativeTransition(ctx context.Context, p identity.Principal, id, session, action string, input NativeDelegationInput, apply func(context.Context, *domain.DelegationRecord, time.Time) error) (DelegationView, error) {
	if session == "" || !validReceptionID(input.DeviceID) || !validDelegationHash(input.Proof) ||
		!validDelegationHash(input.InputHash) {
		return DelegationView{}, apperrors.ErrInvalidArgument
	}
	return s.change(ctx, p, id, action, func(ctx context.Context, item *domain.DelegationRecord, now time.Time) error {
		registered, err := s.store.GetManagedDevice(ctx, p.UserID)
		if err != nil {
			return err
		}
		if registered.DeviceID != input.DeviceID || registered.SessionID != session ||
			!equalDelegationHash(registered.ProofHash, delegationDigest(input.Proof)) {
			return apperrors.ErrAccessDenied
		}
		if !equalDelegationHash(item.InputHash, input.InputHash) ||
			(action != "approve" && (item.DeviceID != input.DeviceID || item.Fence != input.Fence)) {
			return apperrors.ErrConflict
		}
		return apply(ctx, item, now)
	})
}

func (s *Delegations) change(ctx context.Context, p identity.Principal, id, action string, apply func(context.Context, *domain.DelegationRecord, time.Time) error) (DelegationView, error) {
	p, err := s.reception.prepare(ctx, p)
	if err != nil {
		return DelegationView{}, err
	}
	if !validReceptionID(id) {
		return DelegationView{}, apperrors.ErrNotFound
	}
	var view DelegationView
	item, err := s.store.UpdateDelegation(ctx, id, p.UserID, func(ctx context.Context, item *domain.DelegationRecord) error {
		var err error
		view, err = s.view(ctx, *item, p)
		if err != nil {
			return err
		}
		now := nowUTC()
		if !now.Before(item.ExpiresAt) {
			return apperrors.ErrConflict
		}
		if err := apply(ctx, item, now); err != nil {
			return err
		}
		return s.audit(ctx, p, action, *item)
	})
	return delegationView(item, view.CallerName, view.OwnerName), err
}

func (s *Delegations) ticketDigest(ctx context.Context, item *domain.DelegationRecord, token string) (string, error) {
	caller, err := s.reception.identities.CurrentPrincipal(ctx, item.CallerID)
	if err != nil {
		return "", err
	}
	facts, err := s.reception.facts(ctx, caller, item.OwnerID)
	if err != nil {
		return "", err
	}
	if len(facts) == 0 {
		return "", apperrors.ErrNotFound
	}
	// A changed publication set requires a fresh owner approval.
	versions := make([]string, 0, len(facts))
	for _, fact := range facts {
		ref := fact.Citation
		versions = append(versions, ref.AssetID+"/"+strconv.Itoa(ref.Version)+"/"+ref.ObservedAt.Format(time.RFC3339Nano))
	}
	sort.Strings(versions)
	return delegationDigest(token + "\n" + strings.Join(versions, "\n")), nil
}
