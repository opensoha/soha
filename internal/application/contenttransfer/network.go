package contenttransfer

import (
	"context"
	"errors"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"time"
)

type NetworkLinkage interface {
	Within(context.Context, func(context.Context) error) error
	Restrict(context.Context, d.Record) (string, error)
	Release(context.Context, string, string) error
	State(context.Context, string) (d.NetworkHold, error)
}
type Option func(*Service)

func WithNetworkLinkage(link NetworkLinkage) Option { return func(s *Service) { s.linkage = link } }
func (s *Service) authorizeNetworkPolicy(ctx context.Context, p identity.Principal, policy d.Policy) error {
	if policy.NetworkAction != "revoke_network" {
		return nil
	}
	return s.authorizeNetworkLink(ctx, p)
}
func (s *Service) authorizeNetworkLink(ctx context.Context, p identity.Principal) error {
	if s.linkage == nil {
		return apperrors.ErrServiceUnavailable
	}
	return s.global(ctx, p, appdlp.PermissionLink)
}
func (s *Service) recordDenial(ctx context.Context, p identity.Principal, op operation, r d.Record) (d.Result, error) {
	if !r.Private.NetworkRequested {
		if err := s.store.Create(ctx, r); err != nil {
			return d.Result{}, err
		}
		return d.Result{Record: r}, nil
	}
	if s.linkage == nil {
		return d.Result{}, apperrors.ErrServiceUnavailable
	}
	err := s.linkage.Within(ctx, func(linked context.Context) error {
		return s.store.WithCurrent(linked, r.Gates, func(locked context.Context) error {
			if err := s.revalidateOperation(locked, p, &op); err != nil {
				return err
			}
			if err := s.store.Create(locked, r); err != nil {
				return err
			}
			id, err := s.linkage.Restrict(locked, r)
			if err != nil {
				return err
			}
			r.NetworkHoldID = id
			if err := s.store.Update(locked, r, 1, p.UserID, "network_restricted"); err != nil {
				return err
			}
			r.Revision = 2
			r.UpdatedAt = time.Now().UTC()
			return nil
		})
	})
	if err != nil {
		return d.Result{}, err
	}
	return d.Result{Record: r}, nil
}
func (s *Service) NetworkState(ctx context.Context, p identity.Principal, id string) (d.NetworkHold, error) {
	if !validID(id) || p.UserID == "" {
		return d.NetworkHold{}, apperrors.ErrAccessDenied
	}
	r, err := s.store.Record(ctx, id)
	if err != nil {
		return d.NetworkHold{}, err
	}
	if err := s.authorizeHoldRead(ctx, p, r); err != nil {
		return d.NetworkHold{}, err
	}
	if r.NetworkHoldID == "" {
		return d.NetworkHold{}, apperrors.ErrNotFound
	}
	if s.linkage == nil {
		return d.NetworkHold{}, apperrors.ErrServiceUnavailable
	}
	return s.linkage.State(ctx, r.NetworkHoldID)
}
func (s *Service) reviewLinked(ctx context.Context, p identity.Principal, r d.Record, expected int64) error {
	if err := s.authorizeNetworkLink(ctx, p); err != nil {
		return err
	}
	return s.linkage.Within(ctx, func(linked context.Context) error {
		// Releasing a network hold does not revive an expired transfer capability.
		// Lock current policies for review; the stored gates still bind any later token.
		gates := make([]d.Gate, 0, len(r.Gates))
		for _, g := range r.Gates {
			dest, err := s.destination(linked, p, g.DestinationID, Review)
			if err != nil {
				return err
			}
			gates = append(gates, d.Gate{DestinationID: g.DestinationID, PolicyVersion: dest.PublishedVersion})
		}
		return s.store.WithCurrent(linked, gates, func(locked context.Context) error {
			if err := s.store.Update(locked, r, expected, p.UserID, "appeal_reviewed"); err != nil {
				return err
			}
			if r.AppealStatus == "approved" {
				return s.linkage.Release(locked, r.NetworkHoldID, p.UserID)
			}
			return nil
		})
	})
}

func (s *Service) authorizeHoldRead(ctx context.Context, p identity.Principal, r d.Record) error {
	check := func(key string) error {
		if _, err := s.destination(ctx, p, r.DestinationID, key); err != nil {
			return err
		}
		return s.recordScope(ctx, p, r, key)
	}
	if r.ActorID == p.UserID {
		err := check(View)
		if err == nil {
			return nil
		}
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			return err
		}
	}
	if err := s.global(ctx, p, Events); err != nil {
		return err
	}
	return check(Events)
}
